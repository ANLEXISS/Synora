package contract

type Resident struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Role    string `json:"role" yaml:"role"`
	Admin   bool   `json:"admin" yaml:"admin"`

	Contact Contact `json:"contact" yaml:"contact"`

	Baseline Baseline `json:"baseline" yaml:"baseline"`
}

type ResidentView struct {
	ID      string `json:"id" yaml:"id"`
	Name    string `json:"name" yaml:"name"`
	Role    string `json:"role" yaml:"role"`
	Admin   bool   `json:"admin" yaml:"admin"`

	Contact Contact `json:"contact" yaml:"contact"`

	Baseline Baseline `json:"baseline" yaml:"baseline"`
}

type Contact struct {
	WhatsApp string `json:"whatsapp,omitempty" yaml:"whatsapp,omitempty"`
	Phone    string `json:"phone,omitempty" yaml:"phone,omitempty"`
}

type Baseline struct {
	WakeTime  string             `json:"wake_time" yaml:"wake_time"`
	SleepTime string             `json:"sleep_time" yaml:"sleep_time"`
	Rooms     map[string]float64 `json:"rooms" yaml:"rooms"`
}
