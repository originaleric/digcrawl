package media

import "encoding/binary"

// sampleTable validates progressive video data using chunk offsets, the
// sample-to-chunk mapping, and sample sizes.
func (v *mp4Validation) sampleTable(table mp4Box) (bool, error) {
	var sizes, chunks, mapping mp4Box
	var compact, wideOffset bool
	if err := v.boxesIn(table.payload, table.end, func(kind string, box mp4Box) error {
		switch kind {
		case "stsz", "stz2":
			sizes, compact = box, kind == "stz2"
		case "stco", "co64":
			chunks, wideOffset = box, kind == "co64"
		case "stsc":
			mapping = box
		}
		return nil
	}); err != nil {
		return false, err
	}
	if sizes.end == 0 || chunks.end == 0 || mapping.end == 0 {
		return false, errIncompleteMP4
	}
	sampleFields := v.fields(sizes)
	sampleFields.skip(4)
	sizeFormat := sampleFields.u32()
	sampleCount := sampleFields.u32()
	if sampleFields.err != nil {
		return false, sampleFields.err
	}
	if sampleCount == 0 {
		return false, nil // Fragmented files carry sample sizes in trun.
	}
	if err := v.countSamples(sampleCount); err != nil {
		return false, err
	}
	offsets := v.fields(chunks)
	offsets.skip(4)
	chunkCount := offsets.u32()
	runs := v.fields(mapping)
	runs.skip(4)
	entryCount := runs.u32()
	first := runs.u32()
	if first != 1 || entryCount == 0 || entryCount > chunkCount || chunkCount > sampleCount {
		return false, errIncompleteMP4
	}
	var nextChunk uint32 = 1
	var samplesPerChunk uint32
	var consumed uint32
	var hasSamples bool
	var packedByte byte
	for chunk := uint32(1); chunk <= chunkCount; chunk++ {
		if chunk == nextChunk {
			samplesPerChunk = runs.u32()
			runs.skip(4) // sample description index
			entryCount--
			nextChunk = 0
			if entryCount > 0 {
				nextChunk = runs.u32()
				if nextChunk <= chunk || nextChunk > chunkCount {
					return false, errIncompleteMP4
				}
			}
		}
		if samplesPerChunk == 0 || samplesPerChunk > sampleCount-consumed {
			return false, errIncompleteMP4
		}
		offset := uint64(0)
		if wideOffset {
			offset = offsets.u64()
		} else {
			offset = uint64(offsets.u32())
		}
		var length uint64
		switch {
		case !compact && sizeFormat != 0:
			length = uint64(sizeFormat) * uint64(samplesPerChunk)
		case !compact:
			for range samplesPerChunk {
				length += uint64(sampleFields.u32())
			}
		default:
			for i := range samplesPerChunk {
				var data [2]byte
				switch byte(sizeFormat) {
				case 4:
					if (consumed+i)%2 == 0 {
						sampleFields.read(data[:1])
						packedByte = data[0]
						length += uint64(packedByte >> 4)
					} else {
						length += uint64(packedByte & 15)
					}
				case 8:
					sampleFields.read(data[:1])
					length += uint64(data[0])
				case 16:
					sampleFields.read(data[:])
					length += uint64(binary.BigEndian.Uint16(data[:]))
				default:
					return false, errIncompleteMP4
				}
			}
		}
		if offsets.err != nil || runs.err != nil || sampleFields.err != nil || !v.containsData(offset, length) {
			return false, errIncompleteMP4
		}
		hasSamples = hasSamples || length > 0
		consumed += samplesPerChunk
	}
	if consumed != sampleCount {
		return false, errIncompleteMP4
	}
	return hasSamples, nil
}

// fragment checks each track run. Track defaults come from tfhd, then trex.
func (v *mp4Validation) fragment(fragment mp4Box, videoTracks map[uint32]bool, defaultSizes map[uint32]uint32) (bool, error) {
	var videoSamples bool
	nextTrackBase := uint64(fragment.start)
	err := v.boxesIn(fragment.payload, fragment.end, func(kind string, track mp4Box) error {
		if kind != "traf" {
			return nil
		}
		base, runEnd := nextTrackBase, nextTrackBase
		var id, defaultSize uint32
		return v.boxesIn(track.payload, track.end, func(kind string, box mp4Box) error {
			switch kind {
			case "tfhd":
				fields := v.fields(box)
				flags := fields.u32()
				id = fields.u32()
				defaultSize = defaultSizes[id]
				switch {
				case flags&0x01 != 0:
					base = fields.u64()
				case flags&0x020000 != 0: // default-base-is-moof
					base = uint64(fragment.start)
				}
				runEnd = base
				if flags&0x02 != 0 {
					fields.skip(4)
				}
				if flags&0x08 != 0 {
					fields.skip(4)
				}
				if flags&0x10 != 0 {
					defaultSize = fields.u32()
				}
				return fields.err
			case "trun":
				fields := v.fields(box)
				if id == 0 {
					return errIncompleteMP4
				}
				flags, count := fields.u32(), fields.u32()
				if err := v.countSamples(count); err != nil {
					return err
				}
				offset := runEnd
				if flags&0x01 != 0 {
					delta := int64(int32(fields.u32()))
					if delta < 0 && uint64(-delta) > base || delta > 0 && uint64(delta) > ^uint64(0)-base {
						return errIncompleteMP4
					}
					offset = base + uint64(delta)
				}
				if flags&0x04 != 0 {
					fields.skip(4)
				}
				length := uint64(defaultSize) * uint64(count)
				if flags&0x200 != 0 {
					length = 0
				}
				if flags&0xf00 != 0 {
					for range count {
						if flags&0x100 != 0 {
							fields.skip(4)
						}
						if flags&0x200 != 0 {
							length += uint64(fields.u32())
						}
						if flags&0x400 != 0 {
							fields.skip(4)
						}
						if flags&0x800 != 0 {
							fields.skip(4)
						}
					}
				}
				if fields.err != nil || (count != 0 && (length == 0 || !v.containsData(offset, length))) {
					return errIncompleteMP4
				}
				runEnd = offset + length
				nextTrackBase = runEnd
				videoSamples = videoSamples || (videoTracks[id] && count > 0)
			}
			return nil
		})
	})
	return videoSamples, err
}
