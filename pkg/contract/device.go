package contract

type Device struct {
	ID   string
	Type string
	Room string
}

type Registry struct {
	devices map[string]*Device
}
