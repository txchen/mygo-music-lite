package player

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"strings"
)

const (
	maxHLSMetadata   = 1 << 20
	maxHLSSegment    = 8 << 20
	maxHLSReferences = 8192
)

var errNotIndexed = errors.New("audio layout cannot be adapted to internal HLS")
var errInvalidMP4 = errors.New("invalid indexed MP4 audio")

// byteSpan is a checked, nonempty extent inside one upstream representation.
type byteSpan struct{ offset, length int64 }

func checkedSpan(offset, length, total int64) (byteSpan, error) {
	if offset < 0 || length <= 0 || offset > total || length > total-offset {
		return byteSpan{}, errInvalidMP4
	}
	return byteSpan{offset, length}, nil
}

type hlsSegment struct {
	span  byteSpan
	ticks uint32
}
type fragmentDefaults struct{ track, size, duration uint32 }

// hlsAsset is complete before publication and never mutated by handlers.
type hlsAsset struct {
	init, manifest []byte
	segments       []hlsSegment
	timescale      uint32
	defaults       fragmentDefaults
}
type mp4Box struct {
	kind               string
	start, header, end int
	data               []byte
}

func boxAt(b []byte, at int) (mp4Box, error) {
	if at < 0 || len(b)-at < 8 {
		return mp4Box{}, errInvalidMP4
	}
	n, h := uint64(binary.BigEndian.Uint32(b[at:])), 8
	if n == 1 {
		if len(b)-at < 16 {
			return mp4Box{}, errInvalidMP4
		}
		n = binary.BigEndian.Uint64(b[at+8:])
		h = 16
	}
	if n == 0 {
		return mp4Box{}, errNotIndexed
	}
	if n < uint64(h) || n > uint64(len(b)-at) {
		return mp4Box{}, errInvalidMP4
	}
	end := at + int(n)
	return mp4Box{string(b[at+4 : at+8]), at, h, end, b[at+h : end]}, nil
}
func boxes(b []byte) ([]mp4Box, error) {
	var out []mp4Box
	for at := 0; at < len(b); {
		if len(out) == 128 {
			return nil, errNotIndexed
		}
		x, e := boxAt(b, at)
		if e != nil {
			return nil, e
		}
		out = append(out, x)
		at = x.end
	}
	return out, nil
}
func oneBox(b []byte, kind string) (mp4Box, error) {
	xs, e := boxes(b)
	if e != nil {
		return mp4Box{}, e
	}
	var found *mp4Box
	for _, x := range xs {
		if x.kind == kind {
			if found != nil {
				return mp4Box{}, errNotIndexed
			}
			copy := x
			found = &copy
		}
	}
	if found == nil {
		return mp4Box{}, errNotIndexed
	}
	return *found, nil
}
func u32(b []byte) uint32 { return binary.BigEndian.Uint32(b) }
func fullVersion(b []byte, version byte) bool {
	return len(b) >= 4 && b[0] == version && b[1] == 0 && b[2] == 0 && b[3] == 0
}

func normalizeDuration(b []byte, track bool) (uint32, error) {
	if len(b) < 4 {
		return 0, errInvalidMP4
	}
	off, width, id := 16, 4, 12
	if b[0] == 1 {
		off, width, id = 24, 8, 20
	} else if b[0] != 0 {
		return 0, errNotIndexed
	}
	if track {
		off += 4
	}
	if len(b) < off+width {
		return 0, errInvalidMP4
	}
	var trackID uint32
	if track {
		trackID = u32(b[id:])
	}
	clear(b[off : off+width])
	return trackID, nil
}

func parseInit(init []byte) (fragmentDefaults, error) {
	var defaults fragmentDefaults
	f, e := oneBox(init, "ftyp")
	if e != nil {
		return defaults, e
	}
	if len(f.data) < 8 || (len(f.data)-8)%4 != 0 {
		return defaults, errInvalidMP4
	}
	iso6 := string(f.data[:4]) == "iso6"
	for p := 8; p < len(f.data); p += 4 {
		iso6 = iso6 || string(f.data[p:p+4]) == "iso6"
	}
	if !iso6 {
		return defaults, errNotIndexed
	}
	m, e := oneBox(init, "moov")
	if e != nil {
		return defaults, e
	}
	children, e := boxes(m.data)
	if e != nil {
		return defaults, e
	}
	for _, x := range children {
		switch x.kind {
		case "mvhd", "trak", "mvex", "udta":
		default:
			return defaults, errNotIndexed
		}
	}
	mvhd, e := oneBox(m.data, "mvhd")
	if e != nil {
		return defaults, e
	}
	if _, e = normalizeDuration(mvhd.data, false); e != nil {
		return defaults, e
	}
	trak, e := oneBox(m.data, "trak")
	if e != nil {
		return defaults, e
	}
	ts, e := boxes(trak.data)
	if e != nil {
		return defaults, e
	}
	for _, x := range ts {
		if x.kind != "tkhd" && x.kind != "mdia" {
			return defaults, errNotIndexed
		}
	}
	tkhd, e := oneBox(trak.data, "tkhd")
	if e != nil {
		return defaults, e
	}
	defaults.track, e = normalizeDuration(tkhd.data, true)
	if e != nil {
		return defaults, e
	}
	if defaults.track == 0 {
		return defaults, errInvalidMP4
	}
	mdia, e := oneBox(trak.data, "mdia")
	if e != nil {
		return defaults, e
	}
	hdlr, e := oneBox(mdia.data, "hdlr")
	if e != nil {
		return defaults, e
	}
	if len(hdlr.data) < 12 {
		return defaults, errInvalidMP4
	}
	if !fullVersion(hdlr.data, 0) || string(hdlr.data[8:12]) != "soun" {
		return defaults, errNotIndexed
	}
	mdhd, e := oneBox(mdia.data, "mdhd")
	if e != nil {
		return defaults, e
	}
	scaleAt := 12
	if len(mdhd.data) < 4 {
		return defaults, errInvalidMP4
	}
	if mdhd.data[0] == 1 {
		scaleAt = 20
	} else if mdhd.data[0] != 0 {
		return defaults, errNotIndexed
	}
	if len(mdhd.data) < scaleAt+4 || u32(mdhd.data[scaleAt:]) == 0 {
		return defaults, errInvalidMP4
	}
	minf, e := oneBox(mdia.data, "minf")
	if e != nil {
		return defaults, e
	}
	dinf, e := oneBox(minf.data, "dinf")
	if e != nil {
		return defaults, e
	}
	dref, e := oneBox(dinf.data, "dref")
	if e != nil {
		return defaults, e
	}
	if len(dref.data) < 8 || !fullVersion(dref.data, 0) || u32(dref.data[4:]) != 1 {
		return defaults, errNotIndexed
	}
	refs, e := boxes(dref.data[8:])
	if e != nil {
		return defaults, e
	}
	if len(refs) != 1 || refs[0].kind != "url " || len(refs[0].data) != 4 || u32(refs[0].data) != 1 {
		return defaults, errNotIndexed
	}
	stbl, e := oneBox(minf.data, "stbl")
	if e != nil {
		return defaults, e
	}
	tables, e := boxes(stbl.data)
	if e != nil {
		return defaults, e
	}
	for _, x := range tables {
		switch x.kind {
		case "stsd":
		case "stts", "stsc", "stco":
			if len(x.data) != 8 || !fullVersion(x.data, 0) || u32(x.data[4:]) != 0 {
				return defaults, errNotIndexed
			}
		case "stsz":
			if len(x.data) != 12 || !fullVersion(x.data, 0) || u32(x.data[4:]) != 0 || u32(x.data[8:]) != 0 {
				return defaults, errNotIndexed
			}
		default:
			return defaults, errNotIndexed
		}
	}
	for _, kind := range []string{"stts", "stsc", "stco", "stsz"} {
		if _, e = oneBox(stbl.data, kind); e != nil {
			return defaults, e
		}
	}
	stsd, e := oneBox(stbl.data, "stsd")
	if e != nil {
		return defaults, e
	}
	if len(stsd.data) < 8 || !fullVersion(stsd.data, 0) || u32(stsd.data[4:]) != 1 {
		return defaults, errNotIndexed
	}
	entries, e := boxes(stsd.data[8:])
	if e != nil {
		return defaults, e
	}
	if len(entries) != 1 || entries[0].kind != "mp4a" {
		return defaults, errNotIndexed
	}
	sample := entries[0].data
	if len(sample) < 28 {
		return defaults, errInvalidMP4
	}
	if binary.BigEndian.Uint16(sample[6:]) != 1 || u32(sample[8:]) != 0 {
		return defaults, errNotIndexed
	}
	codec, e := boxes(sample[28:])
	if e != nil {
		return defaults, e
	}
	if len(codec) != 1 || codec[0].kind != "esds" || len(codec[0].data) < 4 {
		return defaults, errNotIndexed
	}
	mvex, e := oneBox(m.data, "mvex")
	if e != nil {
		return defaults, e
	}
	exts, e := boxes(mvex.data)
	if e != nil {
		return defaults, e
	}
	if len(exts) != 1 || exts[0].kind != "trex" {
		return defaults, errNotIndexed
	}
	trex := exts[0].data
	if len(trex) != 24 || !fullVersion(trex, 0) {
		return defaults, errInvalidMP4
	}
	if u32(trex[4:]) != defaults.track || u32(trex[8:]) != 1 {
		return defaults, errNotIndexed
	}
	defaults.duration, defaults.size = u32(trex[12:]), u32(trex[16:])
	return defaults, nil
}

func parseIndexedMP4(metadata []byte, total int64) (*hlsAsset, error) {
	xs, e := boxes(metadata)
	if e != nil {
		return nil, e
	}
	if len(xs) != 3 || xs[0].kind != "ftyp" || xs[1].kind != "moov" || xs[2].kind != "sidx" {
		return nil, errNotIndexed
	}
	index := xs[2]
	init := append([]byte(nil), metadata[:index.start]...)
	defaults, e := parseInit(init)
	if e != nil {
		return nil, e
	}
	b := index.data
	if len(b) < 24 {
		return nil, errInvalidMP4
	}
	if b[1] != 0 || b[2] != 0 || b[3] != 0 || b[0] > 1 {
		return nil, errNotIndexed
	}
	scale := u32(b[8:])
	if scale == 0 {
		return nil, errInvalidMP4
	}
	if u32(b[4:]) != defaults.track {
		return nil, errNotIndexed
	}
	var earliest, first uint64
	p := 24
	if b[0] == 0 {
		earliest, first = uint64(u32(b[12:])), uint64(u32(b[16:]))
	} else {
		if len(b) < 32 {
			return nil, errInvalidMP4
		}
		earliest, first = binary.BigEndian.Uint64(b[12:]), binary.BigEndian.Uint64(b[20:])
		p = 32
	}
	if earliest != 0 {
		return nil, errNotIndexed
	}
	count := int(binary.BigEndian.Uint16(b[p-2:]))
	if count == 0 || len(b) != p+12*count {
		return nil, errInvalidMP4
	}
	if count > maxHLSReferences {
		return nil, errNotIndexed
	}
	if first > math.MaxInt64-uint64(index.end) {
		return nil, errInvalidMP4
	}
	offset := int64(index.end) + int64(first)
	asset := &hlsAsset{init: init, timescale: scale, defaults: defaults}
	var maxTicks uint32
	for i := 0; i < count; i++ {
		ref := b[p+i*12:]
		n, ticks, sap := u32(ref), u32(ref[4:]), u32(ref[8:])
		if n>>31 != 0 {
			return nil, errNotIndexed
		}
		if ticks == 0 {
			return nil, errInvalidMP4
		}
		if sap&0x80000000 == 0 || (sap>>28)&7 < 1 || (sap>>28)&7 > 2 || sap&0x0fffffff != 0 {
			return nil, errNotIndexed
		}
		if n > maxHLSSegment {
			return nil, errNotIndexed
		}
		span, e := checkedSpan(offset, int64(n), total)
		if e != nil {
			return nil, e
		}
		asset.segments = append(asset.segments, hlsSegment{span, ticks})
		offset += int64(n)
		maxTicks = max(maxTicks, ticks)
	}
	var manifest strings.Builder
	fmt.Fprintf(&manifest, "#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-MAP:URI=\"init.mp4\"\n", (uint64(maxTicks)+uint64(scale)-1)/uint64(scale))
	for i, s := range asset.segments {
		fmt.Fprintf(&manifest, "#EXTINF:%.9f,\nseg/%d.m4s\n", float64(s.ticks)/float64(scale), i)
	}
	manifest.WriteString("#EXT-X-ENDLIST\n")
	asset.manifest = []byte(manifest.String())
	return asset, nil
}

// validateFragment proves all sample bytes are addressed within this standalone segment.
func validateFragment(b []byte, defaults fragmentDefaults) error {
	xs, e := boxes(b)
	if e != nil {
		return e
	}
	var moof *mp4Box
	var media []byteSpan
	for _, x := range xs {
		switch x.kind {
		case "moof":
			if moof != nil {
				return errNotIndexed
			}
			copy := x
			moof = &copy
		case "mdat":
			media = append(media, byteSpan{int64(x.start + x.header), int64(len(x.data))})
		case "styp", "free":
		default:
			return errNotIndexed
		}
	}
	if moof == nil || len(media) != 1 || media[0].offset < int64(moof.end) {
		return errNotIndexed
	}
	children, e := boxes(moof.data)
	if e != nil {
		return e
	}
	for _, x := range children {
		if x.kind != "mfhd" && x.kind != "traf" {
			return errNotIndexed
		}
	}
	mfhd, e := oneBox(moof.data, "mfhd")
	if e != nil {
		return e
	}
	if len(mfhd.data) != 8 || !fullVersion(mfhd.data, 0) {
		return errInvalidMP4
	}
	traf, e := oneBox(moof.data, "traf")
	if e != nil {
		return e
	}
	parts, e := boxes(traf.data)
	if e != nil {
		return e
	}
	for _, x := range parts {
		if x.kind != "tfhd" && x.kind != "tfdt" && x.kind != "trun" {
			return errNotIndexed
		}
	}
	tfhd, e := oneBox(traf.data, "tfhd")
	if e != nil {
		return e
	}
	h := tfhd.data
	if len(h) < 8 {
		return errInvalidMP4
	}
	flags := u32(h) & 0xffffff
	if h[0] != 0 || flags&0x020000 == 0 || flags & ^uint32(0x02003a) != 0 || u32(h[4:]) != defaults.track {
		return errNotIndexed
	}
	pos := 8
	size := defaults.size
	for _, flag := range []uint32{2, 8, 16, 32} {
		if flags&flag != 0 {
			if len(h)-pos < 4 {
				return errInvalidMP4
			}
			v := u32(h[pos:])
			if flag == 2 && v != 1 {
				return errNotIndexed
			}
			if flag == 16 {
				size = v
			}
			pos += 4
		}
	}
	if pos != len(h) {
		return errInvalidMP4
	}
	tfdt, e := oneBox(traf.data, "tfdt")
	if e != nil {
		return e
	}
	if !(len(tfdt.data) == 8 && fullVersion(tfdt.data, 0) || len(tfdt.data) == 12 && fullVersion(tfdt.data, 1)) {
		return errNotIndexed
	}
	var ends []byteSpan
	runs := 0
	for _, x := range parts {
		if x.kind != "trun" {
			continue
		}
		runs++
		h = x.data
		if len(h) < 12 {
			return errInvalidMP4
		}
		flags = u32(h) & 0xffffff
		if h[0] > 1 || flags&1 == 0 || flags & ^uint32(0xf05) != 0 {
			return errNotIndexed
		}
		count := u32(h[4:])
		if count == 0 || count > 65536 {
			return errNotIndexed
		}
		start := int64(moof.start) + int64(int32(u32(h[8:])))
		pos = 12
		if flags&4 != 0 {
			pos += 4
		}
		width := 0
		for _, flag := range []uint32{0x100, 0x200, 0x400, 0x800} {
			if flags&flag != 0 {
				width += 4
			}
		}
		if uint64(pos)+uint64(count)*uint64(width) != uint64(len(h)) {
			return errInvalidMP4
		}
		var length int64
		for i := uint32(0); i < count; i++ {
			sampleSize := size
			for _, flag := range []uint32{0x100, 0x200, 0x400, 0x800} {
				if flags&flag != 0 {
					if flag == 0x200 {
						sampleSize = u32(h[pos:])
					}
					pos += 4
				}
			}
			if sampleSize == 0 {
				return errNotIndexed
			}
			length += int64(sampleSize)
			if length > int64(len(b)) {
				return errInvalidMP4
			}
		}
		span, e := checkedSpan(start, length, int64(len(b)))
		if e != nil {
			return e
		}
		m := media[0]
		if span.offset < m.offset || span.offset+span.length > m.offset+m.length {
			return errInvalidMP4
		}
		for _, old := range ends {
			if span.offset < old.offset+old.length && old.offset < span.offset+span.length {
				return errNotIndexed
			}
		}
		ends = append(ends, span)
	}
	if runs == 0 {
		return errNotIndexed
	}
	return nil
}
