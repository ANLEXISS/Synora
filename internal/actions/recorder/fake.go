package recorder

import "context"

type Request struct {
	Channel string

	Residents []string

	Value any
}

type FakeRecorder struct {
	Requests []Request

	Details map[string]any

	Err error
}

func (r *FakeRecorder) Record(_ context.Context, channel string, residents []string, value any) (map[string]any, error) {
	if r.Err != nil {
		return nil, r.Err
	}

	r.Requests = append(r.Requests, Request{
		Channel:   channel,
		Residents: append([]string(nil), residents...),
		Value:     value,
	})

	details := map[string]any{}
	for key, value := range r.Details {
		details[key] = value
	}
	return details, nil
}
