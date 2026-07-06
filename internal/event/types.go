package event


type EventType string

const (
	EventMotion  EventType = "vision.motion"
	EventFace    EventType = "vision.face"
	EventAudio   EventType = "device.audio"
	EventDoor    EventType = "device.door"
	EventSystem  EventType = "system.state.changed"
)
