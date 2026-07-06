package contract

type Automation struct {
	ID string
	State string
	Node string
	Conditions []Condition
	Actions []Action
}

type AutomationView struct {
	ID string
	State string
	Node string
	Conditions []Condition
	Actions []Action
}

type Condition struct {
	Field string `yaml:"field"`
	Op    string `yaml:"op"`
	Value any    `yaml:"value"`
}

