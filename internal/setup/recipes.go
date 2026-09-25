package setup

import (
	"fmt"

	"github.com/olostan/DevCadence/internal/protocol"
)

// NewCreateDirectoryAction constructs a versioned executable action for creating a managed directory.
func NewCreateDirectoryAction(actionID string, location protocol.ManagedDirectoryLocation, recipeSetVersion string) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindCreateDirectory,
		CreateDirectory: &protocol.CreateDirectoryParams{
			Location:    location,
			FileModeOct: "0700",
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      fmt.Sprintf("recipe.mkdir.%s", location),
		RecipeVersion: recipeSetVersion,
		Title:         fmt.Sprintf("Create managed directory: %s", location),
		Description:   fmt.Sprintf("Creates %s under $DEVCADENCE_HOME with 0700 permissions", location),
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		Preconditions: []protocol.Condition{},
		Postconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindManagedDirExists,
				ManagedDirExists: &protocol.ManagedDirOperand{
					Location:    location,
					FileModeOct: "0700",
				},
			},
		},
		ExpectedMutations: []protocol.ExpectedMutation{
			{
				Kind:   protocol.MutationDirectoryCreated,
				Target: string(location),
				Detail: "Created directory with permissions 0700",
			},
		},
		IdempotencyKey: fmt.Sprintf("create_dir_%s", location),
	}
}

// NewWriteManagedConfigAction constructs a versioned executable action for updating a managed config key.
func NewWriteManagedConfigAction(actionID string, key protocol.ManagedConfigKey, value string, dependsOn []string, recipeSetVersion string) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindWriteManagedConfig,
		WriteManagedConfig: &protocol.WriteManagedConfigParams{
			Key:   key,
			Value: value,
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      fmt.Sprintf("recipe.config.%s", key),
		RecipeVersion: recipeSetVersion,
		Title:         fmt.Sprintf("Set configuration: %s", key),
		Description:   fmt.Sprintf("Records %s = %s in managed configuration", key, value),
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		DependsOn:     dependsOn,
		// Restores the frozen EWP §3.1 contract (independent-review
		// follow-up on WP-M3B-6, contract-drift cleanup): dependsOn
		// already orders this after the state-directory creation action
		// (see planner.go's dirActionIDs threading), so this precondition
		// is always satisfiable by the time this action runs — it is not
		// asking the applier to do something the dependency graph doesn't
		// already guarantee.
		Preconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindManagedDirExists,
				ManagedDirExists: &protocol.ManagedDirOperand{
					Location:    protocol.LocationState,
					FileModeOct: "0700",
				},
			},
		},
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
				Kind:   protocol.MutationConfigKeySet,
				Target: string(key),
				Detail: fmt.Sprintf("Set %s to %s", key, value),
			},
		},
		IdempotencyKey: fmt.Sprintf("config_set_%s_%s", key, value),
	}
}

// NewRemoveStaleCacheAction constructs a versioned executable action for evicting stale cache targets.
func NewRemoveStaleCacheAction(actionID string, target protocol.CacheTarget, dependsOn []string, recipeSetVersion string) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindRemoveStaleCache,
		RemoveStaleCache: &protocol.RemoveStaleCacheParams{
			Target: target,
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      fmt.Sprintf("recipe.cache.remove.%s", target),
		RecipeVersion: recipeSetVersion,
		Title:         fmt.Sprintf("Remove stale cache: %s", target),
		Description:   fmt.Sprintf("Evicts stale or expired cache for target %s", target),
		Authority:     auth,
		Effects:       effects,
		Operation:     &op,
		DependsOn:     dependsOn,
		// Restores the frozen EWP §3.1 contract (independent-review
		// follow-up on WP-M3B-6, contract-drift cleanup) — see
		// NewWriteManagedConfigAction's identical comment.
		Preconditions: []protocol.Condition{
			{
				Kind: protocol.CondKindManagedDirExists,
				ManagedDirExists: &protocol.ManagedDirOperand{
					Location:    protocol.LocationState,
					FileModeOct: "0700",
				},
			},
		},
		Postconditions: []protocol.Condition{},
		ExpectedMutations: []protocol.ExpectedMutation{
			{
				Kind:   protocol.MutationCacheRemoved,
				Target: string(target),
				Detail: fmt.Sprintf("Removed stale cache %s", target),
			},
		},
		IdempotencyKey: fmt.Sprintf("remove_cache_%s", target),
	}
}

// NewRunDiagnosticCheckAction constructs a versioned executable action for executing a diagnostic probe.
func NewRunDiagnosticCheckAction(actionID string, checkName protocol.DiagnosticCheckName, targetID string, recipeSetVersion string) protocol.SetupAction {
	op := protocol.TypedOperation{
		Kind: protocol.OpKindRunDiagnosticCheck,
		RunDiagnosticCheck: &protocol.RunDiagnosticCheckParams{
			CheckName: checkName,
			TargetID:  targetID,
		},
	}
	effects, auth := protocol.IntrinsicPolicy(op)
	return protocol.SetupAction{
		ActionID:          actionID,
		RecipeID:          fmt.Sprintf("recipe.diagnostic.%s", checkName),
		RecipeVersion:     recipeSetVersion,
		Title:             fmt.Sprintf("Run diagnostic probe: %s", checkName),
		Description:       fmt.Sprintf("Executes non-mutating diagnostic check %s", checkName),
		Authority:         auth,
		Effects:           effects,
		Operation:         &op,
		Preconditions:     []protocol.Condition{},
		Postconditions:    []protocol.Condition{},
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("diagnostic_%s", checkName),
	}
}

// NewManualGitAction constructs a high-impact manual action for installing Git.
func NewManualGitAction(actionID string, recipeSetVersion string) protocol.SetupAction {
	cond := protocol.Condition{
		Kind: protocol.CondKindCommandAvailable,
		CommandAvailable: &protocol.CommandAvailableOperand{
			CommandName: "git",
		},
	}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.install_git",
		RecipeVersion: recipeSetVersion,
		Title:         "Install Git",
		Description:   "Git is required for repository isolation and worktree management",
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectPackageDownload, protocol.EffectFilesystemWrite},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Install Git on the host system",
			Steps: []string{
				"Install Git using your system package manager (e.g., brew install git or apt-get install git)",
				"Alternatively, on macOS run: xcode-select --install",
			},
			VerificationCheck: []protocol.Condition{cond},
		},
		Postconditions:    []protocol.Condition{cond},
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    "manual_install_git",
	}
}

// NewManualReauthenticateAction constructs a high-impact manual action for re-authenticating an expired endpoint.
func NewManualReauthenticateAction(actionID, endpointID, credentialRef, recipeSetVersion string) protocol.SetupAction {
	cond := protocol.Condition{
		Kind: protocol.CondKindEndpointAuthenticated,
		EndpointAuthenticated: &protocol.EndpointOperand{
			EndpointID:      endpointID,
			CredentialRefID: credentialRef,
		},
	}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.reauthenticate",
		RecipeVersion: recipeSetVersion,
		Title:         fmt.Sprintf("Re-authenticate: %s", endpointID),
		Description:   fmt.Sprintf("Authentication for endpoint %s has expired", endpointID),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects:       []protocol.EffectCategory{protocol.EffectAuthentication},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: fmt.Sprintf("Re-authenticate the CLI or update credentials for %s", endpointID),
			Steps: []string{
				fmt.Sprintf("Run the login/authentication command for %s (e.g. its CLI's own login subcommand)", endpointID),
				"Re-run devcadence doctor to confirm the endpoint reports authenticated",
			},
			VerificationCheck: []protocol.Condition{cond},
		},
		Postconditions:    []protocol.Condition{cond},
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("reauthenticate_%s", endpointID),
	}
}

// nvidiaControlNodePath is the NVIDIA proprietary driver's global control
// device — the same path internal/environment/compatibility.go's
// nvidiaCandidate already checks for driver-bound/control-node
// accessibility, reused here so the manual recipe verifies the identical
// real-world fact its own Description claims to remediate.
const nvidiaControlNodePath = "/dev/nvidiactl"

// rocmComputeNodePath is the ROCm/KFD compute device — the same path
// amdCandidates already checks.
const rocmComputeNodePath = "/dev/kfd"

// NewManualNvidiaDriverAction constructs a high-impact manual action for installing proprietary NVIDIA drivers.
func NewManualNvidiaDriverAction(actionID, deviceID, recipeSetVersion string) protocol.SetupAction {
	// Verifies both that the proprietary kernel driver is bound to the
	// specific device in sysfs, and that the control node exists as a
	// device special file.
	condDriver := protocol.Condition{
		Kind: protocol.CondKindKernelDriverBound,
		KernelDriverBound: &protocol.KernelDriverBoundOperand{
			DeviceID: deviceID,
			Driver:   "nvidia",
		},
	}
	condNode := protocol.Condition{
		Kind: protocol.CondKindDeviceNodeAccessible,
		DeviceNodeAccessible: &protocol.DeviceNodeOperand{
			Path: nvidiaControlNodePath,
		},
	}
	verificationConds := []protocol.Condition{condDriver, condNode}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.install_nvidia_driver",
		RecipeVersion: recipeSetVersion,
		Title:         "Install NVIDIA proprietary drivers",
		Description:   fmt.Sprintf("NVIDIA GPU device %s is present but proprietary kernel driver is not bound", deviceID),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects: []protocol.EffectCategory{
			protocol.EffectPrivilegeElevation,
			protocol.EffectDevicePermissionChange,
			protocol.EffectPackageDownload,
		},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Install official NVIDIA proprietary drivers for hardware acceleration",
			Steps: []string{
				"Download and install the appropriate NVIDIA Linux driver for your GPU (e.g., sudo apt-get install nvidia-driver-535 or distro package)",
				"Reboot system to load the proprietary nvidia kernel modules",
				"Confirm driver operation by running nvidia-smi",
			},
			VerificationCheck: verificationConds,
		},
		Postconditions:    verificationConds,
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("manual_install_nvidia_driver_%s", deviceID),
	}
}

// NewManualNvidiaDevicePermissionsAction constructs a high-impact manual action for configuring access to NVIDIA control nodes.
func NewManualNvidiaDevicePermissionsAction(actionID, deviceID, recipeSetVersion string) protocol.SetupAction {
	// RequireAccessible: existence alone is not the fact this recipe
	// remediates — the driver is already bound (that's why this recipe,
	// not install_nvidia_driver, was planned); what must be verified is
	// specifically that the current user can now open the node
	// (independent-review follow-up on WP-M3B-6, FIX_NOW 4).
	cond := protocol.Condition{
		Kind: protocol.CondKindDeviceNodeAccessible,
		DeviceNodeAccessible: &protocol.DeviceNodeOperand{
			Path:              nvidiaControlNodePath,
			RequireAccessible: true,
		},
	}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.configure_nvidia_device_permissions",
		RecipeVersion: recipeSetVersion,
		Title:         "Configure NVIDIA device permissions",
		Description:   fmt.Sprintf("NVIDIA driver is bound for device %s but /dev/nvidiactl is not accessible by current user", deviceID),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects: []protocol.EffectCategory{
			protocol.EffectPrivilegeElevation,
			protocol.EffectDevicePermissionChange,
		},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Grant user permissions to access NVIDIA GPU device nodes",
			Steps: []string{
				"Add current user to 'video' or 'render' group: sudo usermod -aG video,render $USER",
				"Ensure udev rules permit rw access to /dev/nvidia* for group render/video",
				"Log out and log back in to apply group changes",
			},
			VerificationCheck: []protocol.Condition{cond},
		},
		Postconditions:    []protocol.Condition{cond},
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("manual_nvidia_device_access_%s", deviceID),
	}
}

// NewManualRocmDriverAction constructs a high-impact manual action for installing AMD ROCm drivers and compute stack.
func NewManualRocmDriverAction(actionID, deviceID, recipeSetVersion string) protocol.SetupAction {
	// Verifies both that the amdgpu kernel driver is bound to the
	// specific device in sysfs, and that the /dev/kfd compute node
	// exists as a device special file.
	condDriver := protocol.Condition{
		Kind: protocol.CondKindKernelDriverBound,
		KernelDriverBound: &protocol.KernelDriverBoundOperand{
			DeviceID: deviceID,
			Driver:   "amdgpu",
		},
	}
	condNode := protocol.Condition{
		Kind: protocol.CondKindDeviceNodeAccessible,
		DeviceNodeAccessible: &protocol.DeviceNodeOperand{
			Path: rocmComputeNodePath,
		},
	}
	verificationConds := []protocol.Condition{condDriver, condNode}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.install_rocm_driver",
		RecipeVersion: recipeSetVersion,
		Title:         "Install AMD ROCm compute drivers",
		Description:   fmt.Sprintf("AMD GPU device %s is present but ROCm compute stack is not installed", deviceID),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects: []protocol.EffectCategory{
			protocol.EffectPrivilegeElevation,
			protocol.EffectDevicePermissionChange,
			protocol.EffectPackageDownload,
		},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Install AMD ROCm kernel driver and compute runtime",
			Steps: []string{
				"Follow AMD documentation to install rocm-hip-sdk or amdgpu-dkms from official AMD package repositories",
				"Reboot system to load the updated amdgpu and amdkfd kernel modules",
				"Confirm ROCm installation with rocminfo",
			},
			VerificationCheck: verificationConds,
		},
		Postconditions:    verificationConds,
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("manual_install_rocm_driver_%s", deviceID),
	}
}

// NewManualAmdgpuDevicePermissionsAction constructs a high-impact manual action for configuring AMD /dev/kfd device permissions.
func NewManualAmdgpuDevicePermissionsAction(actionID, deviceID, recipeSetVersion string) protocol.SetupAction {
	// RequireAccessible: the driver is already bound (that's why this
	// recipe, not install_rocm_driver, was planned); what must be
	// verified is specifically that the current user can now open the
	// node (independent-review follow-up on WP-M3B-6, FIX_NOW 4).
	cond := protocol.Condition{
		Kind: protocol.CondKindDeviceNodeAccessible,
		DeviceNodeAccessible: &protocol.DeviceNodeOperand{
			Path:              rocmComputeNodePath,
			RequireAccessible: true,
		},
	}
	return protocol.SetupAction{
		ActionID:      actionID,
		RecipeID:      "recipe.manual.configure_amdgpu_device_permissions",
		RecipeVersion: recipeSetVersion,
		Title:         "Configure AMD GPU device permissions",
		Description:   fmt.Sprintf("AMD GPU device %s is present but /dev/kfd or /dev/dri is not accessible by current user", deviceID),
		Authority:     protocol.AuthorityHighImpactManual,
		Effects: []protocol.EffectCategory{
			protocol.EffectPrivilegeElevation,
			protocol.EffectDevicePermissionChange,
		},
		Preconditions: []protocol.Condition{},
		ManualInstructions: &protocol.ManualGuide{
			Summary: "Grant user permissions to access AMD ROCm device node (/dev/kfd)",
			Steps: []string{
				"Add current user to 'render' group: sudo usermod -aG render $USER",
				"Log out and log back in to apply group changes",
			},
			VerificationCheck: []protocol.Condition{cond},
		},
		Postconditions:    []protocol.Condition{cond},
		ExpectedMutations: []protocol.ExpectedMutation{},
		IdempotencyKey:    fmt.Sprintf("manual_amdgpu_device_access_%s", deviceID),
	}
}
