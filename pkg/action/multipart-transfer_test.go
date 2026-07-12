package action

import "testing"

func TestMultipartPartSizeHonorsS3Limits(t *testing.T) {
	if got := multipartPartSize(1, 0); got != minMultipartPartSize {
		t.Fatalf("small requested part size = %d, want %d", got, minMultipartPartSize)
	}
	tooLargeForDefault := defaultMultipartSize*maxMultipartParts + 1
	got := multipartPartSize(int(defaultMultipartSize/(1024*1024)), tooLargeForDefault)
	if got*maxMultipartParts < tooLargeForDefault {
		t.Fatalf("part size %d cannot fit %d bytes within %d parts", got, tooLargeForDefault, maxMultipartParts)
	}
}
