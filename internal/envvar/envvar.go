package envvar

// Cloud Foundry environment variables understood by the official CLI.
const (
	CFHome       = "CF_HOME"
	CFPluginHome = "CF_PLUGIN_HOME"
	CFTrace      = "CF_TRACE"
)

// cfs environment variables form part of the public CLI contract.
const (
	ActiveContext = "CFS_ACTIVE_CONTEXT"
	ConfigFile    = "CFS_CONFIG_FILE"
	Disable       = "CFS_DISABLE"
	LockTimeout   = "CFS_LOCK_TIMEOUT"
	NoUpdateCheck = "CFS_NO_UPDATE_CHECK"
	ShimDir       = "CFS_SHIM_DIR"
	StateHome     = "CFS_STATE_HOME"
	WorkspaceRoot = "CFS_WORKSPACE_ROOT"
)

// Platform environment variables used to follow operating-system conventions.
const (
	CI           = "CI"
	LocalAppData = "LOCALAPPDATA"
	XDGStateHome = "XDG_STATE_HOME"
)
