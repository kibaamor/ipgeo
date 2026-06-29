package downloader

import "testing"

func TestProgressGroup_NotTTY(t *testing.T) {
	pg := NewProgressGroup()
	if pg.p != nil {
		t.Fatal("non-TTY ProgressGroup should have nil internal Progress")
	}

	bar := pg.AddBar("test")
	if bar.bar != nil {
		t.Fatal("non-TTY ProgressBar should have nil internal Bar")
	}

	bar.SetTotal(100)
	bar.SetCurrent(50)
	bar.MarkDone(0)
	pg.Wait()
}
