package setup

import (
	"fmt"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

const DefaultRecipeSetVersion = "1.0.0"

// Standard model parameters for Ollama pull recipe
const (
	DefaultOllamaModelTag     = "qwen2.5-coder:7b"
	DefaultOllamaDigest       = "sha256:e9f1a0e1c26c718a38a719c2f6d22efd332616f9f60f64c1257fa23f793b1373"
	DefaultOllamaSizeBytes    = 4700000000
	DefaultOllamaRegistryHost = "registry.ollama.ai"
	DefaultOllamaLicense      = "Apache-2.0"
)

// PlannerOptions configures the remediation planner.
type PlannerOptions struct {
	Clock            clock.Clock
	IDs              ids.Source
	RecipeSetVersion string
	Facts            *protocol.EnvironmentFacts
	SelectedRuntimes []string
}

// Planner generates an immutable SetupPlan from a DoctorReport and setup target.
type Planner struct {
	clock            clock.Clock
	ids              ids.Source
	recipeSetVersion string
	facts            *protocol.EnvironmentFacts
	selectedRuntimes []string
}

// NewPlanner returns a Planner.
func NewPlanner(opts PlannerOptions) (*Planner, error) {
	if opts.Clock == nil {
		opts.Clock = clock.System()
	}
	if opts.IDs == nil {
		opts.IDs = ids.NewULIDSource()
	}
	if opts.RecipeSetVersion == "" {
		opts.RecipeSetVersion = DefaultRecipeSetVersion
	}
	return &Planner{
		clock:            opts.Clock,
		ids:              opts.IDs,
		recipeSetVersion: opts.RecipeSetVersion,
		facts:            opts.Facts,
		selectedRuntimes: opts.SelectedRuntimes,
	}, nil
}

// Plan constructs a SetupPlan to address findings in the DoctorReport.
func (p *Planner) Plan(report *protocol.DoctorReport, target protocol.SetupTarget, profile protocol.DeploymentProfile) (*protocol.SetupPlan, error) {
	if report == nil {
		return nil, errs.New(errs.CategoryInvalidArgument, "setup planner: doctor report is required")
	}
	if !target.Valid() {
		return nil, errs.New(errs.CategoryInvalidArgument, "setup planner: invalid target %q", target)
	}
	if profile == "" && report.RecommendedProfile != nil && report.RecommendedProfile.SelectedProfile != nil {
		profile = *report.RecommendedProfile.SelectedProfile
	}

	var actions []protocol.SetupAction
	actionIndex := 1

	// Check state root directories
	needsDirs := false
	for _, f := range report.Findings {
		if f.Code == FindingCodeStateDirsMissing {
			needsDirs = true
			break
		}
	}

	var dirActionIDs []string
	if needsDirs && (target == protocol.TargetAll || target == protocol.TargetHardware) {
		dirs := []protocol.ManagedDirectoryLocation{
			protocol.LocationState,
			protocol.LocationArtifactsSetup,
			protocol.LocationTmp,
		}
		for _, loc := range dirs {
			actID := fmt.Sprintf("act_%s_%04d", loc, actionIndex)
			actionIndex++
			dirActionIDs = append(dirActionIDs, actID)

			op := protocol.TypedOperation{
				Kind: protocol.OpKindCreateDirectory,
				CreateDirectory: &protocol.CreateDirectoryParams{
					Location:    loc,
					FileModeOct: "0700",
				},
			}
			effects, auth := protocol.IntrinsicPolicy(op)
			actions = append(actions, protocol.SetupAction{
				ActionID:      actID,
				RecipeID:      fmt.Sprintf("recipe.mkdir.%s", loc),
				RecipeVersion: p.recipeSetVersion,
				Title:         fmt.Sprintf("Create managed directory: %s", loc),
				Description:   fmt.Sprintf("Creates %s under $DEVCADENCE_HOME with 0700 permissions", loc),
				Authority:     auth,
				Effects:       effects,
				Operation:     &op,
				Postconditions: []protocol.Condition{
					{
						Kind: protocol.CondKindManagedDirExists,
						ManagedDirExists: &protocol.ManagedDirOperand{
							Location:    loc,
							FileModeOct: "0700",
						},
					},
				},
				ExpectedMutations: []protocol.ExpectedMutation{
					{
						Kind:   "directory_created",
						Target: string(loc),
						Detail: "Created directory with permissions 0700",
					},
				},
				IdempotencyKey: fmt.Sprintf("create_dir_%s", loc),
			})
		}
	}

	// Check Git
	needsGit := false
	for _, f := range report.Findings {
		if f.Code == FindingCodeGitNotFound {
			needsGit = true
			break
		}
	}
	if needsGit && (target == protocol.TargetAll || target == protocol.TargetHardware) {
		actID := fmt.Sprintf("act_git_%04d", actionIndex)
		actionIndex++
		actions = append(actions, protocol.SetupAction{
			ActionID:      actID,
			RecipeID:      "recipe.manual.install_git",
			RecipeVersion: p.recipeSetVersion,
			Title:         "Install Git",
			Description:   "Git is required for repository isolation and worktree management",
			Authority:     protocol.AuthorityHighImpactManual,
			Effects:       []protocol.EffectCategory{protocol.EffectPackageDownload, protocol.EffectFilesystemWrite},
			ManualInstructions: &protocol.ManualGuide{
				Summary: "Install Git on the host system",
				Steps: []string{
					"Install Git using your system package manager (e.g., brew install git or apt-get install git)",
					"Alternatively, on macOS run: xcode-select --install",
				},
				VerificationCheck: []protocol.Condition{
					{
						Kind: protocol.CondKindCommandAvailable,
						CommandAvailable: &protocol.CommandAvailableOperand{
							CommandName: "git",
						},
					},
				},
			},
			Postconditions: []protocol.Condition{
				{
					Kind: protocol.CondKindCommandAvailable,
					CommandAvailable: &protocol.CommandAvailableOperand{
						CommandName: "git",
					},
				},
			},
			IdempotencyKey: "manual_install_git",
		})
	}

	// Check Cognition: Pull model for local runtime if profile uses local execution
	if (profile == protocol.ProfileLocalHeavy || profile == protocol.ProfileHybridThin || profile == protocol.ProfileOffline) &&
		(target == protocol.TargetAll || target == protocol.TargetCognition || target == protocol.TargetInference) {

		shouldPullOllama := false
		for _, ep := range report.DiscoveredEndpoints {
			if ep.Kind == protocol.EndpointLocalRuntime &&
				(ep.Health == protocol.EndpointHealthReady || ep.Health == protocol.EndpointHealthNotConfigured) &&
				strings.Contains(strings.ToLower(ep.ID), "ollama") {
				shouldPullOllama = true
				break
			}
		}

		if shouldPullOllama {
			trustworthyIdentity := false
			var ollamaPath string
			var ollamaVersion string

			if p.facts != nil {
				factsFp, fpErr := environment.Fingerprint(*p.facts)
				if fpErr == nil && factsFp == report.MachineFingerprint {
					for _, sw := range p.facts.Software {
						if sw.ID == "ollama" && sw.Installed && sw.Path != "" && sw.Version != "" {
							ollamaPath = sw.Path
							ollamaVersion = sw.Version
							trustworthyIdentity = true
							break
						}
					}
				}
			}

			actID := fmt.Sprintf("act_pull_model_%04d", actionIndex)
			actionIndex++

			if trustworthyIdentity {
				op := protocol.TypedOperation{
					Kind: protocol.OpKindOllamaPullModel,
					OllamaPullModel: &protocol.OllamaPullModelParams{
						ModelTag:            DefaultOllamaModelTag,
						ResolvedDigest:      DefaultOllamaDigest,
						ExpectedSizeBytes:   DefaultOllamaSizeBytes,
						AllowedRegistryHost: DefaultOllamaRegistryHost,
						LicenseReference:    DefaultOllamaLicense,
					},
				}
				effects, auth := protocol.IntrinsicPolicy(op)
				actions = append(actions, protocol.SetupAction{
					ActionID:      actID,
					RecipeID:      "recipe.ollama.pull_model",
					RecipeVersion: p.recipeSetVersion,
					Title:         fmt.Sprintf("Pull model: %s", DefaultOllamaModelTag),
					Description:   fmt.Sprintf("Pulls verified local coding model %s via Ollama", DefaultOllamaModelTag),
					Authority:     auth,
					Effects:       effects,
					Operation:     &op,
					DependsOn:     dirActionIDs,
					Preconditions: []protocol.Condition{
						{
							Kind: protocol.CondKindCommandAvailable,
							CommandAvailable: &protocol.CommandAvailableOperand{
								CommandName: "ollama",
							},
						},
						{
							Kind: protocol.CondKindExecutableVerified,
							ExecutableVerified: &protocol.ExecutableVerifiedOperand{
								CanonicalPath:   ollamaPath,
								ExpectedVersion: ollamaVersion,
							},
						},
						{
							Kind: protocol.CondKindPortListening,
							PortListening: &protocol.PortOperand{
								Host: "127.0.0.1",
								Port: 11434,
							},
						},
					},
					Postconditions: []protocol.Condition{
						{
							Kind: protocol.CondKindModelDigestPresent,
							ModelDigestPresent: &protocol.ModelDigestOperand{
								Runtime:  "ollama",
								ModelTag: DefaultOllamaModelTag,
								Digest:   DefaultOllamaDigest,
							},
						},
					},
					ExpectedMutations: []protocol.ExpectedMutation{
						{
							Kind:   "model_pulled",
							Target: DefaultOllamaModelTag,
							Detail: fmt.Sprintf("Model %s with digest %s pulled to local storage", DefaultOllamaModelTag, DefaultOllamaDigest),
						},
					},
					IdempotencyKey: fmt.Sprintf("ollama_pull_%s", DefaultOllamaModelTag),
				})
			} else {
				actions = append(actions, protocol.SetupAction{
					ActionID:      actID,
					RecipeID:      "recipe.manual.pull_ollama_model",
					RecipeVersion: p.recipeSetVersion,
					Title:         fmt.Sprintf("Pull model manually: %s", DefaultOllamaModelTag),
					Description:   fmt.Sprintf("Verified executable identity for Ollama is unavailable; manual model pull is required for %s", DefaultOllamaModelTag),
					Authority:     protocol.AuthorityHighImpactManual,
					Effects:       []protocol.EffectCategory{protocol.EffectPackageDownload, protocol.EffectFilesystemWrite},
					DependsOn:     dirActionIDs,
					ManualInstructions: &protocol.ManualGuide{
						Summary: fmt.Sprintf("Pull %s using Ollama CLI", DefaultOllamaModelTag),
						Steps: []string{
							"Ensure Ollama is running and accessible",
							fmt.Sprintf("Run: ollama pull %s", DefaultOllamaModelTag),
						},
						VerificationCheck: []protocol.Condition{
							{
								Kind: protocol.CondKindModelDigestPresent,
								ModelDigestPresent: &protocol.ModelDigestOperand{
									Runtime:  "ollama",
									ModelTag: DefaultOllamaModelTag,
									Digest:   DefaultOllamaDigest,
								},
							},
						},
					},
					Postconditions: []protocol.Condition{
						{
							Kind: protocol.CondKindModelDigestPresent,
							ModelDigestPresent: &protocol.ModelDigestOperand{
								Runtime:  "ollama",
								ModelTag: DefaultOllamaModelTag,
								Digest:   DefaultOllamaDigest,
							},
						},
					},
					ExpectedMutations: []protocol.ExpectedMutation{
						{
							Kind:   "model_pulled",
							Target: DefaultOllamaModelTag,
							Detail: fmt.Sprintf("Model %s with digest %s pulled to local storage", DefaultOllamaModelTag, DefaultOllamaDigest),
						},
					},
					IdempotencyKey: fmt.Sprintf("manual_ollama_pull_%s", DefaultOllamaModelTag),
				})
			}
		}

		// Check MLX setup: Gate to compatible Darwin/arm64 systems
		isDarwinArm64 := p.facts != nil && p.facts.Host.Family == protocol.OSDarwin && p.facts.Host.Arch == "arm64"

		mlxSelected := false
		for _, r := range p.selectedRuntimes {
			if strings.EqualFold(r, "mlx") || strings.EqualFold(r, "mlx-lm") {
				mlxSelected = true
				break
			}
		}

		mlxDetected := false
		for _, ep := range report.DiscoveredEndpoints {
			if strings.Contains(strings.ToLower(ep.ID), "mlx") {
				mlxDetected = true
				break
			}
		}
		if !mlxDetected && p.facts != nil {
			for _, sw := range p.facts.Software {
				if (sw.ID == "mlx" || sw.ID == "mlx-lm") && sw.Installed {
					mlxDetected = true
					break
				}
			}
		}

		if isDarwinArm64 && (mlxDetected || mlxSelected) {
			actID := fmt.Sprintf("act_setup_mlx_%04d", actionIndex)
			actionIndex++
			actions = append(actions, protocol.SetupAction{
				ActionID:      actID,
				RecipeID:      "recipe.manual.setup_mlx",
				RecipeVersion: p.recipeSetVersion,
				Title:         "Set up MLX local inference",
				Description:   "Guides manual configuration and verification of MLX local inference on Apple Silicon",
				Authority:     protocol.AuthorityHighImpactManual,
				Effects:       []protocol.EffectCategory{protocol.EffectPackageDownload, protocol.EffectFilesystemWrite},
				DependsOn:     dirActionIDs,
				ManualInstructions: &protocol.ManualGuide{
					Summary: "Configure MLX local runtime on Apple Silicon",
					Steps: []string{
						"Install mlx-lm in a dedicated Python environment (e.g., pip install mlx-lm)",
						"Launch the MLX model server or configure DevCadence MLX adapter endpoint",
					},
					VerificationCheck: []protocol.Condition{
						{
							Kind: protocol.CondKindCommandAvailable,
							CommandAvailable: &protocol.CommandAvailableOperand{
								CommandName: "mlx_lm.server",
							},
						},
					},
				},
				Postconditions: []protocol.Condition{
					{
						Kind: protocol.CondKindCommandAvailable,
						CommandAvailable: &protocol.CommandAvailableOperand{
							CommandName: "mlx_lm.server",
						},
					},
				},
				IdempotencyKey: "manual_setup_mlx",
			})
		}
	}

	// Write managed config for selected profile
	if profile.Valid() && (target == protocol.TargetAll || target == protocol.TargetCognition) {
		actID := fmt.Sprintf("act_config_%04d", actionIndex)
		actionIndex++
		op := protocol.TypedOperation{
			Kind: protocol.OpKindWriteManagedConfig,
			WriteManagedConfig: &protocol.WriteManagedConfigParams{
				Key:   protocol.ConfigKeyDefaultProfile,
				Value: string(profile),
			},
		}
		effects, auth := protocol.IntrinsicPolicy(op)
		actions = append(actions, protocol.SetupAction{
			ActionID:      actID,
			RecipeID:      "recipe.config.default_profile",
			RecipeVersion: p.recipeSetVersion,
			Title:         fmt.Sprintf("Set default profile: %s", profile),
			Description:   fmt.Sprintf("Records default profile %s in managed configuration", profile),
			Authority:     auth,
			Effects:       effects,
			Operation:     &op,
			DependsOn:     dirActionIDs,
			Postconditions: []protocol.Condition{
				{
					Kind: protocol.CondKindManagedDirExists,
					ManagedDirExists: &protocol.ManagedDirOperand{
						Location:    protocol.LocationState,
						FileModeOct: "0700",
					},
				},
			},
			ExpectedMutations: []protocol.ExpectedMutation{
				{
					Kind:   "config_key_set",
					Target: string(protocol.ConfigKeyDefaultProfile),
					Detail: fmt.Sprintf("Set default_profile to %s", profile),
				},
			},
			IdempotencyKey: fmt.Sprintf("config_set_default_profile_%s", profile),
		})
	}

	// Determine required authority and total effects
	var reqAuth protocol.Authority = protocol.AuthorityReadOnly
	effectMap := make(map[protocol.EffectCategory]struct{})
	for _, act := range actions {
		if act.Authority.Rank() > reqAuth.Rank() {
			reqAuth = act.Authority
		}
		for _, eff := range act.Effects {
			effectMap[eff] = struct{}{}
		}
	}

	var totalEffects []protocol.EffectCategory
	for eff := range effectMap {
		totalEffects = append(totalEffects, eff)
	}
	sort.Slice(totalEffects, func(i, j int) bool {
		return totalEffects[i] < totalEffects[j]
	})

	plan := &protocol.SetupPlan{
		SchemaVersion:      protocol.SchemaVersion1,
		PlanID:             p.ids.New("pln"),
		RecipeSetVersion:   p.recipeSetVersion,
		MachineFingerprint: report.MachineFingerprint,
		CreatedAt:          protocol.NewTimestamp(p.clock.Now()),
		Target:             target,
		Actions:            actions,
		RequiredAuthority:  reqAuth,
		TotalEffects:       totalEffects,
	}

	digest, err := plan.ComputePlanDigest()
	if err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "compute plan digest")
	}
	plan.PlanDigest = digest

	if err := plan.Validate(); err != nil {
		return nil, errs.Wrap(errs.CategoryInternal, err, "generated setup plan failed validation")
	}

	return plan, nil
}
