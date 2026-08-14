package contract

import "testing"

func TestFacePhotoStatusTransitions(t *testing.T) {
	allowed := [][2]FacePhotoStatus{{FacePhotoReceiving, FacePhotoStored}, {FacePhotoStored, FacePhotoValidating}, {FacePhotoValidating, FacePhotoActive}, {FacePhotoActive, FacePhotoRemovalPending}, {FacePhotoRemovalPending, FacePhotoRemoved}, {FacePhotoRejected, FacePhotoRemoved}}
	for _, pair := range allowed {
		if !ValidFacePhotoTransition(pair[0], pair[1]) {
			t.Fatalf("transition %s -> %s rejected", pair[0], pair[1])
		}
	}
	if ValidFacePhotoTransition(FacePhotoActive, FacePhotoStored) {
		t.Fatal("inverse transition accepted")
	}
	if err := FacePhotoStatus("bad").Validate(); err == nil {
		t.Fatal("invalid status accepted")
	}
}
