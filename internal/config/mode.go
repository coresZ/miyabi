package config

// RuntimeMode distinguishes how the application is being hosted so that
// desktop-only behaviour can be enabled without changing the defaults of the
// console binary or the Docker image.
type RuntimeMode int

const (
	// RuntimeServer is the default: the console binary or the Docker image.
	RuntimeServer RuntimeMode = iota
	// RuntimeDesktop is the Wails desktop shell hosting the same services.
	RuntimeDesktop
)
