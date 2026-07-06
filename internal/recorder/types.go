package recorder

type Clip struct {
	ID       string
	CameraID string
	Path     string
	Start    int64
	End      int64
}
