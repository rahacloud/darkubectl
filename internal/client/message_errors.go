package client

import "errors"

var (
	errDeploymentTypeChangedSinceItWasLoaded   = errors.New("deployment type changed since it was loaded; refresh before saving")
	errGitBuildSettingsChangedSinceTheyWere    = errors.New("git build settings changed since they were loaded; refresh before saving")
	errPlatformAcceptedTheGitSettingsUpdateBut = errors.New("platform accepted the Git settings update but did not store all requested values")
)
