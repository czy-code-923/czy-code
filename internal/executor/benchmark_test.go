package executor

import (
	"os"
	"path/filepath"
	"testing"
	"vps-control/internal/manifest"
)

func BenchmarkBinaryDigest(b *testing.B) {
	p := filepath.Join(b.TempDir(), "engine")
	if e := os.WriteFile(p, make([]byte, 16<<20), 0600); e != nil {
		b.Fatal(e)
	}
	b.Run("previous-read-all", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for range b.N {
			data, e := os.ReadFile(p)
			if e != nil {
				b.Fatal(e)
			}
			_ = manifest.Digest(data)
		}
	})
	b.Run("streaming", func(b *testing.B) {
		b.SetBytes(16 << 20)
		b.ReportAllocs()
		for range b.N {
			if _, e := FileDigest(p); e != nil {
				b.Fatal(e)
			}
		}
	})
}
