package vcjob

import "testing"

func TestApplyScheduleMetadataAnnotationsWritesTolerance(t *testing.T) {
	annotations := map[string]string{}

	ApplyScheduleMetadataAnnotations(annotations, ptrToInt64(180))
	if got := annotations[AnnotationKeyWaitingToleranceSeconds]; got != "180" {
		t.Fatalf("waiting tolerance annotation = %q, want %q", got, "180")
	}
}

func TestApplyScheduleMetadataAnnotationsClearsToleranceWhenAbsent(t *testing.T) {
	annotations := map[string]string{AnnotationKeyWaitingToleranceSeconds: "180"}

	ApplyScheduleMetadataAnnotations(annotations, nil)
	if _, ok := annotations[AnnotationKeyWaitingToleranceSeconds]; ok {
		t.Fatal("a job without a tolerance must not carry the annotation, otherwise it would time out")
	}
}

func ptrToInt64(value int64) *int64 {
	return &value
}
