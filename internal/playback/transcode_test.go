package playback

import (
	"context"
	"errors"
	"testing"

	"github.com/ppxb/miyabi/internal/domain"
	"github.com/ppxb/miyabi/internal/drive"
	"github.com/ppxb/miyabi/internal/pan"
)

// 115 answers with no transcoded sources for a while after an upload. That is
// an upstream condition the UI must explain, not an internal failure: the raw
// pan sentinel has to surface as the drive error with Kind=Upstream (HTTP 502).
func TestStartReportsMissingTranscodeAsUpstream(t *testing.T) {
	service, source := playFixture(t)
	stub := stubOf(t, service.drive)
	stub.info = func(_ context.Context, _, id string) (pan.FileInfo, error) {
		return pan.FileInfo{
			File: pan.File{ID: id, ParentID: source.Directory.ID, Name: "ABP-001-CD1.mp4", PickCode: "pick-101"},
			Path: []pan.Directory{{ID: source.Directory.ID, Name: source.Directory.Name}},
		}, nil
	}
	stub.playURL = func(context.Context, string, string) ([]pan.PlaySource, error) {
		return nil, pan.ErrTranscodeUnavailable
	}

	_, err := service.Start(t.Context(), "101")
	if !errors.Is(err, drive.ErrTranscodeUnavailable) {
		t.Fatalf("missing transcode did not map to the drive sentinel: %v", err)
	}
	if kind := domain.KindOf(err); kind != domain.KindUpstream {
		t.Fatalf("missing transcode must be an upstream error, got kind %v: %v", kind, err)
	}
	if message := domain.PublicMessage(err); message == "" || message == err.Error() {
		t.Fatalf("missing transcode must carry a user-facing message, got %q", message)
	}
}
