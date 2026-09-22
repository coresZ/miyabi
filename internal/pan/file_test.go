package pan

import (
	"bytes"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestFileListDecodesNumericAndStringCountsAndSizes(t *testing.T) {
	for _, test := range []struct {
		name      string
		body      string
		wantTotal int
		wantSize  int64
	}{
		{
			name: "numeric count and size",
			body: `{"state":true,"code":0,"cid":"123","count":42,"data":[{"fid":"f1","pid":"123","fn":"movie.mp4","fc":"1","fs":1048576,"pc":"pick1","sha1":"abc"}],"path":[{"cid":"123","name":"media"}]}`,
			wantTotal: 42,
			wantSize:  1048576,
		},
		{
			name: "string count and size",
			body: `{"state":true,"code":0,"cid":"123","count":"99","data":[{"fid":"f2","pid":"123","fn":"movie2.mp4","fc":"1","fs":"2097152","pc":"pick2","sha1":"def"}],"path":[{"cid":"123","name":"media"}]}`,
			wantTotal: 99,
			wantSize:  2097152,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := New()
			defer client.Close()
			client.http.SetTransport(offlineRoundTrip(func(request *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Header:     http.Header{"Content-Type": {"application/json"}},
					Body:       io.NopCloser(strings.NewReader(test.body)),
					Request:    request,
				}, nil
			}))

			page, err := client.List(t.Context(), "token", "123", 0, 10)
			if err != nil {
				t.Fatalf("List error = %v", err)
			}
			if page.Total != test.wantTotal {
				t.Errorf("Total = %d, want %d", page.Total, test.wantTotal)
			}
			if len(page.Files) != 1 || page.Files[0].Size != test.wantSize {
				t.Errorf("File size = %d, want %d", page.Files[0].Size, test.wantSize)
			}
		})
	}
}

func TestUploadMetadataReusesSHA1ForSmallBody(t *testing.T) {
	client := New()
	defer client.Close()

	body := bytes.Repeat([]byte("a"), 1024) // 1KiB <= 128KiB
	expectedHash := SHA1(body)

	client.http.SetTransport(offlineRoundTrip(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path == "/open/upload/init" {
			if err := request.ParseForm(); err != nil {
				t.Fatal(err)
			}
			fileid := request.Form.Get("fileid")
			preid := request.Form.Get("preid")
			if fileid != expectedHash || preid != expectedHash {
				t.Errorf("fileid=%s preid=%s, want both=%s", fileid, preid, expectedHash)
			}
			// Status 2 means fast instant upload completed
			return &http.Response{
				StatusCode: http.StatusOK,
				Header:     http.Header{"Content-Type": {"application/json"}},
				Body:       io.NopCloser(strings.NewReader(`{"state":true,"code":0,"data":{"status":2}}`)),
				Request:    request,
			}, nil
		}
		t.Fatalf("unexpected request: %s %s", request.Method, request.URL.Path)
		return nil, nil
	}))

	err := client.UploadMetadata(t.Context(), "token", "123", "meta.nfo", body)
	if err != nil {
		t.Fatalf("UploadMetadata error = %v", err)
	}
}
