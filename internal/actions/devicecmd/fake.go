package devicecmd

import "context"

type Command struct {
	DeviceID string

	Command string

	Value any
}

type FakeCommander struct {
	Commands []Command

	Err error
}

func (c *FakeCommander) Command(_ context.Context, deviceID string, command string, value any) error {
	if c.Err != nil {
		return c.Err
	}

	c.Commands = append(c.Commands, Command{
		DeviceID: deviceID,
		Command:  command,
		Value:    value,
	})
	return nil
}
