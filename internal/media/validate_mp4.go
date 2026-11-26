package media

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

var errIncompleteMP4 = errors.New("MP4 metadata or sample data is incomplete")

type mp4Box struct {
	start, payload, end int64
}

type mp4Range struct {
	start, end uint64
}

type mp4Validation struct {
	file    io.ReaderAt
	boxes   int
	samples uint64
	data    []mp4Range
}

// ValidateMP4 verifies container boundaries and audio/video sample ranges without
// decoding frames or loading media data into memory.
func ValidateMP4(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	return validateMP4File(file)
}

func validateMP4File(file *os.File) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	validator := mp4Validation{file: file}
	return validator.validate(info.Size())
}

func (v *mp4Validation) validate(size int64) error {
	var movie mp4Box
	var fragments []mp4Box
	var fileType bool
	if err := v.boxesIn(0, size, func(kind string, box mp4Box) error {
		switch kind {
		case "ftyp":
			length := box.end - box.payload
			fileType = length >= 8 && length%4 == 0
		case "moov":
			movie = box
		case "moof":
			fragments = append(fragments, box)
		case "mdat":
			v.data = append(v.data, mp4Range{uint64(box.payload), uint64(box.end)})
		}
		return nil
	}); err != nil {
		return err
	}
	if !fileType || movie.end == 0 || len(v.data) == 0 {
		return errIncompleteMP4
	}

	videoTracks := make(map[uint32]bool)
	defaultSizes := make(map[uint32]uint32)
	var movieHeader, videoSamples bool
	if err := v.boxesIn(movie.payload, movie.end, func(kind string, box mp4Box) error {
		switch kind {
		case "mvhd":
			movieHeader = box.end-box.payload >= 100
		case "trak":
			trackID, handler, table, err := v.track(box)
			if err != nil || (handler != "vide" && handler != "soun") {
				return err
			}
			if trackID == 0 || table.end == 0 {
				return errIncompleteMP4
			}
			hasSamples, err := v.sampleTable(table)
			if handler == "vide" {
				videoTracks[trackID] = true
				videoSamples = videoSamples || hasSamples
			}
			return err
		case "mvex":
			return v.boxesIn(box.payload, box.end, func(kind string, box mp4Box) error {
				if kind != "trex" {
					return nil
				}
				fields := v.fields(box)
				fields.skip(4)
				trackID := fields.u32()
				fields.skip(8)
				defaultSizes[trackID] = fields.u32()
				return fields.err
			})
		}
		return nil
	}); err != nil {
		return err
	}
	if !movieHeader || len(videoTracks) == 0 {
		return errIncompleteMP4
	}
	for _, fragment := range fragments {
		hasSamples, err := v.fragment(fragment, videoTracks, defaultSizes)
		if err != nil {
			return err
		}
		videoSamples = videoSamples || hasSamples
	}
	if !videoSamples {
		return errIncompleteMP4
	}
	return nil
}

func (v *mp4Validation) track(track mp4Box) (id uint32, handler string, table mp4Box, err error) {
	err = v.boxesIn(track.payload, track.end, func(kind string, box mp4Box) error {
		switch kind {
		case "tkhd":
			fields := v.fields(box)
			switch fields.u32() >> 24 {
			case 0:
				fields.skip(8)
			case 1:
				fields.skip(16)
			default:
				return errIncompleteMP4
			}
			id = fields.u32()
			return fields.err
		case "mdia":
			return v.boxesIn(box.payload, box.end, func(kind string, box mp4Box) error {
				switch kind {
				case "hdlr":
					fields := v.fields(box)
					fields.skip(8)
					var name [4]byte
					fields.read(name[:])
					handler = string(name[:])
					return fields.err
				case "minf":
					return v.boxesIn(box.payload, box.end, func(kind string, box mp4Box) error {
						if kind == "stbl" {
							table = box
						}
						return nil
					})
				}
				return nil
			})
		}
		return nil
	})
	return
}

// containsData checks that a chunk or fragment run fits within one mdat payload.
func (v *mp4Validation) containsData(start, size uint64) bool {
	i := sort.Search(len(v.data), func(i int) bool { return v.data[i].end >= start })
	return i < len(v.data) && start >= v.data[i].start && size <= v.data[i].end-start
}

func (v *mp4Validation) countSamples(count uint32) error {
	v.samples += uint64(count)
	if v.samples > 5_000_000 {
		return fmt.Errorf("MP4 contains too many samples")
	}
	return nil
}

func (v *mp4Validation) boxesIn(start, end int64, visit func(string, mp4Box) error) error {
	for offset := start; offset < end; {
		v.boxes++
		if v.boxes > 100_000 {
			return fmt.Errorf("MP4 contains too many boxes")
		}
		var header [8]byte
		if end-offset < 8 {
			return errIncompleteMP4
		}
		if _, err := v.file.ReadAt(header[:], offset); err != nil {
			return err
		}
		kind := string(header[4:])
		size := uint64(binary.BigEndian.Uint32(header[:4]))
		headerSize := uint64(8)
		switch size {
		case 0:
			size = uint64(end - offset)
		case 1:
			headerSize += 8
			if uint64(end-offset) < headerSize {
				return errIncompleteMP4
			}
			if _, err := v.file.ReadAt(header[:], offset+8); err != nil {
				return err
			}
			size = binary.BigEndian.Uint64(header[:])
		}
		if kind == "uuid" {
			headerSize += 16
		}
		if size < headerSize || size > uint64(end-offset) {
			return errIncompleteMP4
		}
		next := offset + int64(size)
		if err := visit(kind, mp4Box{offset, offset + int64(headerSize), next}); err != nil {
			return err
		}
		offset = next
	}
	return nil
}

type mp4Fields struct {
	reader *bufio.Reader
	err    error
}

func (v *mp4Validation) fields(box mp4Box) *mp4Fields {
	return &mp4Fields{reader: bufio.NewReader(io.NewSectionReader(v.file, box.payload, box.end-box.payload))}
}

func (f *mp4Fields) read(p []byte) {
	if f.err == nil {
		_, f.err = io.ReadFull(f.reader, p)
	}
}

func (f *mp4Fields) u32() uint32 {
	var data [4]byte
	f.read(data[:])
	return binary.BigEndian.Uint32(data[:])
}

func (f *mp4Fields) u64() uint64 {
	var data [8]byte
	f.read(data[:])
	return binary.BigEndian.Uint64(data[:])
}

func (f *mp4Fields) skip(bytes int) {
	if f.err == nil {
		_, f.err = f.reader.Discard(bytes)
	}
}
