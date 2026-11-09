package player

import (
	"bytes"
	"encoding/binary"
	"errors"
	"strings"
	"testing"
)

func mp4Words(v ...uint32) []byte {
	b := make([]byte, len(v)*4)
	for i, n := range v {
		binary.BigEndian.PutUint32(b[i*4:], n)
	}
	return b
}
func testBox(kind string, parts ...[]byte) []byte {
	body := bytes.Join(parts, nil)
	return bytes.Join([][]byte{mp4Words(uint32(8 + len(body))), []byte(kind), body}, nil)
}
func indexedFixture(version byte, gap int) ([]byte, []byte, []byte) {
	mvhd := make([]byte, 100)
	tkhd := make([]byte, 84)
	mdhd := make([]byte, 24)
	mvhd[0], tkhd[0] = version, version
	if version == 1 {
		mvhd = append(mvhd, make([]byte, 12)...)
		tkhd = append(tkhd, make([]byte, 12)...)
		binary.BigEndian.PutUint32(tkhd[20:], 1)
		binary.BigEndian.PutUint64(mvhd[24:], 9000)
		binary.BigEndian.PutUint64(tkhd[28:], 9000)
	} else {
		binary.BigEndian.PutUint32(tkhd[12:], 1)
		binary.BigEndian.PutUint32(mvhd[16:], 9000)
		binary.BigEndian.PutUint32(tkhd[20:], 9000)
	}
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	sample := make([]byte, 28)
	sample[7] = 1
	stbl := testBox("stbl", testBox("stsd", mp4Words(0, 1), testBox("mp4a", sample, testBox("esds", mp4Words(0)))), testBox("stts", mp4Words(0, 0)), testBox("stsc", mp4Words(0, 0)), testBox("stco", mp4Words(0, 0)), testBox("stsz", mp4Words(0, 0, 0)))
	dinf := testBox("dinf", testBox("dref", mp4Words(0, 1), testBox("url ", mp4Words(1))))
	trak := testBox("trak", testBox("tkhd", tkhd), testBox("mdia", testBox("mdhd", mdhd), testBox("hdlr", mp4Words(0, 0), []byte("soun")), testBox("minf", dinf, stbl)))
	init := bytes.Join([][]byte{testBox("ftyp", []byte("dash"), mp4Words(0), []byte("iso6")), testBox("moov", testBox("mvhd", mvhd), testBox("mvex", testBox("trex", mp4Words(0, 1, 1, 1000, 4, 0))), trak)}, nil)
	fragment := testBox("moof", testBox("mfhd", mp4Words(0, 1)), testBox("traf", testBox("tfhd", mp4Words(0x020000, 1)), testBox("tfdt", mp4Words(0, 0)), testBox("trun", mp4Words(1, 1, 0))))
	// Each trun points at the mdat payload relative to its own moof.
	binary.BigEndian.PutUint32(fragment[len(fragment)-4:], uint32(len(fragment)+8))
	fragment = append(fragment, testBox("mdat", []byte{1, 2, 3, 4})...)
	idx := mp4Words(uint32(version)<<24, 1, 1000)
	if version == 0 {
		idx = append(idx, mp4Words(0, uint32(gap))...)
	} else {
		idx = append(idx, mp4Words(0, 0, 0, uint32(gap))...)
	}
	idx = append(idx, mp4Words(2, uint32(len(fragment)), 1000, 0x90000000, uint32(len(fragment)), 1000, 0x90000000)...)
	metadata := append(append([]byte(nil), init...), testBox("sidx", idx)...)
	second := append([]byte(nil), fragment...)
	all := bytes.Join([][]byte{metadata, make([]byte, gap), fragment, second}, nil)
	return metadata, fragment, all
}

// Flat indexes use the actual first_offset and normalize only versioned movie and track durations.
func TestIndexedMP4Plan(t *testing.T) {
	for _, version := range []byte{0, 1} {
		metadata, fragment, all := indexedFixture(version, 19)
		original := append([]byte(nil), metadata...)
		asset, e := parseIndexedMP4(metadata, int64(len(all)))
		if e != nil {
			t.Fatal(e)
		}
		if !bytes.Equal(metadata, original) {
			t.Fatal("parser changed source metadata")
		}
		if asset.segments[0].span.offset != int64(len(metadata)+19) || asset.segments[1].span.offset != int64(len(metadata)+19+len(fragment)) {
			t.Fatal("first_offset or reference extent lost")
		}
		if !strings.Contains(string(asset.manifest), "#EXTINF:1.000000000,\nseg/1.m4s") || !strings.HasSuffix(string(asset.manifest), "#EXT-X-ENDLIST\n") {
			t.Fatal("manifest lost indexed timeline")
		}
		moov, _ := oneBox(asset.init, "moov")
		mvhd, _ := oneBox(moov.data, "mvhd")
		trak, _ := oneBox(moov.data, "trak")
		tkhd, _ := oneBox(trak.data, "tkhd")
		off, width := 16, 4
		if version == 1 {
			off, width = 24, 8
		}
		if !bytes.Equal(mvhd.data[off:off+width], make([]byte, width)) || !bytes.Equal(tkhd.data[off+4:off+4+width], make([]byte, width)) {
			t.Fatal("duration not normalized")
		}
		if e = validateFragment(fragment, asset.defaults); e != nil {
			t.Fatal(e)
		}
	}
}

// Corrupt box sizes and index extents never become a published media plan or panic.
func TestIndexedMP4RejectsCorruption(t *testing.T) {
	metadata, _, all := indexedFixture(0, 0)
	idx := bytes.Index(metadata, []byte("sidx")) + 4
	for name, mutate := range map[string]func([]byte){
		"zero scale":         func(b []byte) { clear(b[idx+8 : idx+12]) },
		"oversize reference": func(b []byte) { binary.BigEndian.PutUint32(b[idx+24:], uint32(len(all))) },
		"bad count":          func(b []byte) { b[idx+23] = 3 },
		"zero duration":      func(b []byte) { clear(b[idx+28 : idx+32]) },
		"overflow box":       func(b []byte) { binary.BigEndian.PutUint32(b, 0xffffffff) },
	} {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), metadata...)
			mutate(b)
			if _, e := parseIndexedMP4(b, int64(len(all))); e == nil {
				t.Fatal("corruption accepted")
			}
		})
	}
	for n := 0; n < len(metadata); n++ {
		if _, e := parseIndexedMP4(metadata[:n], int64(len(all))); e == nil {
			t.Fatalf("truncation %d accepted", n)
		}
	}
}

// Valid but unsupported index layouts retain the direct-file playback path.
func TestIndexedMP4UnsupportedLayouts(t *testing.T) {
	metadata, _, all := indexedFixture(0, 0)
	idx := bytes.Index(metadata, []byte("sidx")) + 4
	for name, mutate := range map[string]func([]byte){
		"hierarchical":        func(b []byte) { b[idx+24] |= 0x80 },
		"nonzero timeline":    func(b []byte) { b[idx+15] = 1 },
		"nonindependent":      func(b []byte) { b[idx+32] = 0 },
		"unsupported version": func(b []byte) { b[idx] = 2 },
		"encrypted entry":     func(b []byte) { at := bytes.Index(b, []byte("mp4a")); copy(b[at:], "enca") },
	} {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), metadata...)
			mutate(b)
			if _, e := parseIndexedMP4(b, int64(len(all))); !errors.Is(e, errNotIndexed) {
				t.Fatalf("got %v", e)
			}
		})
	}
}

// Standalone fragments cannot reference samples before their mdat or beyond their own bytes.
func TestFragmentAddressing(t *testing.T) {
	metadata, fragment, all := indexedFixture(0, 0)
	asset, e := parseIndexedMP4(metadata, int64(len(all)))
	if e != nil {
		t.Fatal(e)
	}
	for name, mutate := range map[string]func([]byte){
		"absolute base": func(b []byte) { at := bytes.Index(b, []byte("tfhd")) + 4; b[at+3] |= 1 },
		"negative offset": func(b []byte) {
			at := bytes.Index(b, []byte("trun")) + 12
			binary.BigEndian.PutUint32(b[at:], 0xffffffff)
		},
		"past mdat": func(b []byte) {
			at := bytes.Index(b, []byte("trun")) + 12
			binary.BigEndian.PutUint32(b[at:], uint32(len(b)))
		},
		"missing tfdt":  func(b []byte) { at := bytes.Index(b, []byte("tfdt")); copy(b[at:], "free") },
		"wrong track":   func(b []byte) { at := bytes.Index(b, []byte("tfhd")) + 8; binary.BigEndian.PutUint32(b[at:], 2) },
		"invalid count": func(b []byte) { at := bytes.Index(b, []byte("trun")) + 8; binary.BigEndian.PutUint32(b[at:], 65537) },
	} {
		t.Run(name, func(t *testing.T) {
			b := append([]byte(nil), fragment...)
			mutate(b)
			if e := validateFragment(b, asset.defaults); e == nil {
				t.Fatal("unsafe standalone fragment accepted")
			}
		})
	}
}
