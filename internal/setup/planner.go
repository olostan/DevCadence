package setup

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/olostan/DevCadence/internal/clock"
	"github.com/olostan/DevCadence/internal/environment"
	"github.com/olostan/DevCadence/internal/errs"
	"github.com/olostan/DevCadence/internal/ids"
	"github.com/olostan/DevCadence/internal/protocol"
)

const DefaultRecipeSetVersion = "1.0.0"

// Standard model parameters for the Ollama pull recipe.
const (
	DefaultOllamaModelTag     = "qwen2.5-coder:7b"
	DefaultOllamaDigest       = "sha256:e9f1a0e1c26c718a38a719c2f6d22efd332616f9f60f64c1257fa23f793b1373"
	DefaultOllamaSizeBytes    = 4700000000
	DefaultOllamaRegistryHost = "registry.ollama.ai"
	DefaultOllamaLicense      = "Apache-2.0"
)

// Standard model parameters for the MLX (Hugging Face Hub) download recipe
// — MLX-LM's own model distribution path. These are the equal-peer
// counterpart of the DefaultOllama* constants above: neither runtime is
// the "default" one, both are recipe inputs this planner treats
// symmetrically (see modelruntime.go, INVARIANTS.md DCI-055).
//
// DefaultMLXRevision MUST be an immutable Hugging Face commit hash, never
// a mutable ref like "main" — an approved PlanDigest binds this exact
// value, and a mutable branch ref would let the bytes actually downloaded
// diverge from what was approved (the same immutable-plan property
// DefaultOllamaDigest already provides on the Ollama side). This value was
// resolved from the Hugging Face Hub API (`GET /api/models/{id}` →
// `.sha`) against the repo named by DefaultMLXModelRef; re-resolve and
// update both constants together if the recipe's model reference changes.
// DefaultMLXSizeBytes was resolved the same way, from the repo tree at
// that revision (`GET /api/models/{id}/tree/{revision}`, summed file
// sizes) — a real measured value, not an estimate.
const (
	DefaultMLXModelRef  = "mlx-community/Qwen2.5-Coder-7B-Instruct-4bit"
	DefaultMLXRevision  = "019cc73c45c770444708a6dd8690c66243cc5c80"
	DefaultMLXSizeBytes = 4295890004
	DefaultMLXSource    = "huggingface.co"
	DefaultMLXLicense   = "Apache-2.0"
)

// hfCommitHashPattern matches a full Hugging Face/git commit hash (40
// lowercase hex characters) — the only revision form that pins an
// immutable snapshot. Branch/tag refs like "main" or "refs/pr/1" fail
// this, by design.
var hfCommitHashPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)

// isImmutableHFRevision reports whether rev is an immutable Hugging Face
// commit hash rather than a mutable ref. The automated ensure_local_model
// path for any Hugging-Face-backed runtime (MLX today) must never build an
// executable action from a mutable ref — see localModelRecipe's use of
// this in ensureLocalModelAction.
func isImmutableHFRevision(rev string) bool {
	return hfCommitHashPattern.MatchString(rev)
}

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
	// profile is taken exactly as the caller passed it — never defaulted
	// from report.RecommendedProfile.SelectedProfile. RecommendedProfile is
	// an informational UX label (WP-M3B-5 de-authorizes it from readiness
	// and remediation authority); silently promoting it here would let a
	// label the operator never chose decide which SetupActions get planned
	// (independent-review follow-up on WP-M3B-5, finding 1).

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

	// Check auth: one manual re-authenticate action per endpoint whose
	// auth status is expired (WP-M3B-5 §5.3). Endpoints, not Findings text,
	// are the source of truth here — the same DiscoveredEndpoints the
	// Cognition block below already iterates — so this needs no fragile
	// parsing of a finding's free-text Title/Detail to recover which
	// endpoint it was about.
	//
	// Deliberately asymmetric with FindingCodeNoCodingEndpoint (no action
	// generated for that finding at all): AuthExpired names a concrete,
	// already-configured endpoint a human can re-authenticate with a
	// generic instruction; NoCodingEndpoint does not name anything to act
	// on, and inventing a generic "install and authenticate some coding
	// CLI" action would mean recommending a specific provider, which the
	// WP-M3B-5 MUST constraint forbids (see that EWP's §3).
	if target == protocol.TargetAll || target == protocol.TargetAuth {
		for _, ep := range report.DiscoveredEndpoints {
			if ep.Auth != protocol.AuthExpired {
				continue
			}
			actID := fmt.Sprintf("act_auth_%04d", actionIndex)
			actionIndex++
			actions = append(actions, protocol.SetupAction{
				ActionID:      actID,
				RecipeID:      "recipe.manual.reauthenticate",
				RecipeVersion: p.recipeSetVersion,
				Title:         fmt.Sprintf("Re-authenticate: %s", ep.ID),
				Description:   fmt.Sprintf("Authentication for endpoint %s has expired", ep.ID),
				Authority:     protocol.AuthorityHighImpactManual,
				Effects:       []protocol.EffectCategory{protocol.EffectAuthentication},
				ManualInstructions: &protocol.ManualGuide{
					Summary: fmt.Sprintf("Re-authenticate the CLI or update credentials for %s", ep.ID),
					Steps: []string{
						fmt.Sprintf("Run the login/authentication command for %s (e.g. its CLI's own login subcommand)", ep.ID),
						"Re-run devcadence doctor to confirm the endpoint reports authenticated",
					},
					// endpoint_authenticated, not endpoint_healthy: health
					// says nothing about credential validity (WP-M3B-4's
					// "healthy != authenticated != usable" invariant) —
					// using health here would let a re-auth action's
					// postcondition pass while the endpoint is still
					// unauthenticated (independent-review follow-up on
					// WP-M3B-5, finding 6).
					VerificationCheck: []protocol.Condition{
						{
							Kind:                  protocol.CondKindEndpointAuthenticated,
							EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: ep.ID},
						},
					},
				},
				Postconditions: []protocol.Condition{
					{
						Kind:                  protocol.CondKindEndpointAuthenticated,
						EndpointAuthenticated: &protocol.EndpointOperand{EndpointID: ep.ID},
					},
				},
				IdempotencyKey: fmt.Sprintf("reauthenticate_%s", ep.ID),
			})
		}
	}

	// Check Cognition: ensure a local model for whichever local runtime(s)
	// are relevant, treating every runtime the planner knows about as an
	// equal peer (INVARIANTS.md DCI-055, docs/MODEL_RUNTIME.md) — neither
	// Ollama nor MLX gets a distinct code path shape; only their recipe
	// inputs (command name, model ref, detection gate) differ.
	//
	// Gated on target plus concrete missing facts / explicit runtime
	// selection only — never on the informational RecommendedProfile
	// label, which an operator never necessarily chose (independent-review
	// follow-up on WP-M3B-5, finding 1).
	if target == protocol.TargetAll || target == protocol.TargetCognition || target == protocol.TargetInference {

		ollamaSelected := false
		for _, r := range p.selectedRuntimes {
			if strings.EqualFold(r, "ollama") {
				ollamaSelected = true
				break
			}
		}

		ollamaDetected := false
		for _, ep := range report.DiscoveredEndpoints {
			if ep.Kind == protocol.EndpointLocalRuntime &&
				(ep.Health == protocol.EndpointHealthReady || ep.Health == protocol.EndpointHealthNotConfigured) &&
				strings.Contains(strings.ToLower(ep.ID), "ollama") {
				ollamaDetected = true
				break
			}
		}
		shouldPullOllama := ollamaDetected || ollamaSelected
		if shouldPullOllama {
			var ollamaPath, ollamaVersion string
			if p.facts != nil {
				factsFp, fpErr := environment.Fingerprint(*p.facts)
				if fpErr == nil && factsFp == report.MachineFingerprint {
					for _, sw := range p.facts.Software {
						if sw.ID == "ollama" && sw.Installed && sw.Path != "" && sw.Version != "" {
							ollamaPath, ollamaVersion = sw.Path, sw.Version
							break
						}
					}
				}
			}
			action := p.ensureLocalModelAction(&actionIndex, dirActionIDs, localModelRecipe{
				runtime:           "ollama",
				modelRef:          DefaultOllamaModelTag,
				resolvedRevision:  DefaultOllamaDigest,
				expectedSizeBytes: DefaultOllamaSizeBytes,
				allowedSource:     DefaultOllamaRegistryHost,
				licenseReference:  DefaultOllamaLicense,
				commandName:       "ollama",
				executablePath:    ollamaPath,
				executableVersion: ollamaVersion,
				recipeIDAuto:      "recipe.ollama.pull_model",
				recipeIDManual:    "recipe.manual.pull_ollama_model",
				manualSteps: []string{
					"Ensure Ollama is running and accessible",
					fmt.Sprintf("Run: ollama pull %s", DefaultOllamaModelTag),
				},
				extraPreconditions: []protocol.Condition{
					{
						Kind: protocol.CondKindPortListening,
						PortListening: &protocol.PortOperand{
							Host: "127.0.0.1",
							Port: 11434,
						},
					},
				},
			})
			actions = append(actions, action)
		}

		// MLX is only a viable local runtime on Apple Silicon — a real
		// platform constraint, not a preference between MLX and Ollama.
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
			// "hf" is the current Hugging Face Hub CLI; the older
			// "huggingface-cli" name was removed in huggingface_hub v1.0
			// (see internal/setup/mlx_adapter.go's doc comment).
			var hfPath, hfVersion string
			if p.facts != nil {
				factsFp, fpErr := environment.Fingerprint(*p.facts)
				if fpErr == nil && factsFp == report.MachineFingerprint {
					for _, sw := range p.facts.Software {
						if (sw.ID == "hf" || sw.ID == "huggingface_hub") && sw.Installed && sw.Path != "" && sw.Version != "" {
							hfPath, hfVersion = sw.Path, sw.Version
							break
						}
					}
				}
			}
			action := p.ensureLocalModelAction(&actionIndex, dirActionIDs, localModelRecipe{
				runtime:             "mlx",
				modelRef:            DefaultMLXModelRef,
				resolvedRevision:    DefaultMLXRevision,
				expectedSizeBytes:   DefaultMLXSizeBytes,
				allowedSource:       DefaultMLXSource,
				licenseReference:    DefaultMLXLicense,
				commandName:         "hf",
				executablePath:      hfPath,
				executableVersion:   hfVersion,
				recipeIDAuto:        "recipe.mlx.download_model",
				recipeIDManual:      "recipe.manual.pull_mlx_model",
				revisionIsImmutable: isImmutableHFRevision,
				versionProbeKind:    protocol.VersionProbeVersionSubcommand,
				manualSteps: []string{
					"Install mlx-lm and huggingface_hub in a dedicated Python environment (e.g., pip install mlx-lm huggingface_hub)",
					fmt.Sprintf("Run: hf download %s --revision %s", DefaultMLXModelRef, DefaultMLXRevision),
				},
			})
			actions = append(actions, action)
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

// localModelRecipe collects one local model runtime's recipe inputs, so
// ensureLocalModelAction can build an automated-or-manual ensure_local_model
// action identically for every runtime — no runtime's action shape differs
// from another's (INVARIANTS.md DCI-055).
type localModelRecipe struct {
	runtime           string
	modelRef          string
	resolvedRevision  string
	expectedSizeBytes int64
	allowedSource     string
	licenseReference  string
	// commandName is the CLI this runtime's automated path shells out to
	// (e.g. "ollama", "huggingface-cli"), used for the command_available
	// precondition and reported in the manual guide's steps.
	commandName string
	// executablePath/executableVersion are the trustworthy-identity
	// evidence (from environment facts matching the current machine
	// fingerprint) that authorizes the automated path; empty means fall
	// back to a manual action, symmetric across every runtime.
	executablePath    string
	executableVersion string
	recipeIDAuto      string
	recipeIDManual    string
	manualSteps       []string
	// extraPreconditions are appended to the automated action's
	// preconditions beyond command_available/executable_verified (e.g.
	// Ollama's local port check); most runtimes need none.
	extraPreconditions []protocol.Condition
	// revisionIsImmutable, when non-nil, gates the automated path on
	// resolvedRevision actually being an immutable pin (e.g. a Hugging
	// Face commit hash, not a mutable branch ref like "main") — an
	// approved PlanDigest binds resolvedRevision, so a mutable ref would
	// let the bytes downloaded at apply time diverge from what was
	// approved. nil means the runtime's own resolvedRevision format is
	// already inherently immutable (e.g. Ollama's sha256 digest) and
	// needs no separate check.
	revisionIsImmutable func(string) bool
	// versionProbeKind selects, from the closed protocol.VersionProbeKind
	// set, how commandName is asked for its version, mirrored into the
	// automated action's executable_verified precondition — see
	// protocol.VersionProbeKind's doc comment. Empty means the protocol
	// default (VersionProbeDoubleDashVersion).
	versionProbeKind protocol.VersionProbeKind
}

// ensureLocalModelAction builds the action for one localModelRecipe: an
// automated ensure_local_model operation when r.executablePath/Version
// establish a trustworthy identity for r.commandName AND (when
// r.revisionIsImmutable is set) r.resolvedRevision is actually an
// immutable pin, otherwise a manual action with the same real
// postcondition (model_present) — never a weaker command_available proxy,
// for any runtime.
func (p *Planner) ensureLocalModelAction(actionIndex *int, dependsOn []string, r localModelRecipe) protocol.SetupAction {
	actID := fmt.Sprintf("act_pull_model_%s_%04d", r.runtime, *actionIndex)
	*actionIndex++

	postcondition := protocol.Condition{
		Kind: protocol.CondKindModelPresent,
		ModelPresent: &protocol.ModelPresentOperand{
			Runtime:           r.runtime,
			ModelRef:          r.modelRef,
			ResolvedRevision:  r.resolvedRevision,
			ExpectedSizeBytes: r.expectedSizeBytes,
		},
	}
	mutation := protocol.ExpectedMutation{
		Kind:   "model_pulled",
		Target: r.modelRef,
		Detail: fmt.Sprintf("Model %s (%s) pulled to local storage via %s", r.modelRef, r.resolvedRevision, r.runtime),
	}

	trustworthyIdentity := r.executablePath != "" && r.executableVersion != ""
	revisionPinned := r.revisionIsImmutable == nil || r.revisionIsImmutable(r.resolvedRevision)

	if trustworthyIdentity && revisionPinned {
		op := protocol.TypedOperation{
			Kind: protocol.OpKindEnsureLocalModel,
			EnsureLocalModel: &protocol.EnsureLocalModelParams{
				Runtime:           r.runtime,
				ModelRef:          r.modelRef,
				ResolvedRevision:  r.resolvedRevision,
				ExpectedSizeBytes: r.expectedSizeBytes,
				AllowedSource:     r.allowedSource,
				LicenseReference:  r.licenseReference,
			},
		}
		effects, auth := protocol.IntrinsicPolicy(op)
		preconditions := append([]protocol.Condition{
			{
				Kind: protocol.CondKindCommandAvailable,
				CommandAvailable: &protocol.CommandAvailableOperand{
					CommandName: r.commandName,
				},
			},
			{
				Kind: protocol.CondKindExecutableVerified,
				ExecutableVerified: &protocol.ExecutableVerifiedOperand{
					CanonicalPath:   r.executablePath,
					ExpectedVersion: r.executableVersion,
					VersionProbe:    r.versionProbeKind,
				},
			},
		}, r.extraPreconditions...)

		return protocol.SetupAction{
			ActionID:          actID,
			RecipeID:          r.recipeIDAuto,
			RecipeVersion:     p.recipeSetVersion,
			Title:             fmt.Sprintf("Pull model: %s", r.modelRef),
			Description:       fmt.Sprintf("Pulls verified local coding model %s via %s", r.modelRef, r.runtime),
			Authority:         auth,
			Effects:           effects,
			Operation:         &op,
			DependsOn:         dependsOn,
			Preconditions:     preconditions,
			Postconditions:    []protocol.Condition{postcondition},
			ExpectedMutations: []protocol.ExpectedMutation{mutation},
			IdempotencyKey:    fmt.Sprintf("%s_pull_%s", r.runtime, r.modelRef),
		}
	}

	return protocol.SetupAction{
		ActionID:      actID,
		RecipeID:      r.recipeIDManual,
		RecipeVersion: p.recipeSetVersion,
		Title:         fmt.Sprintf("Pull model manually: %s", r.modelRef),
		Description:   fmt.Sprintf("Verified executable identity for %s is unavailable; manual model pull is required for %s", r.runtime, r.modelRef),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectPackageDownload, protocol.EffectFilesystemWrite},
		DependsOn:     dependsOn,
		ManualInstructions: &protocol.ManualGuide{
			Summary:           fmt.Sprintf("Pull %s using the %s CLI", r.modelRef, r.runtime),
			Steps:             r.manualSteps,
			VerificationCheck: []protocol.Condition{postcondition},
		},
		Postconditions:    []protocol.Condition{postcondition},
		ExpectedMutations: []protocol.ExpectedMutation{mutation},
		IdempotencyKey:    fmt.Sprintf("manual_%s_pull_%s", r.runtime, r.modelRef),
	}
}
