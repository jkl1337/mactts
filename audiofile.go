package mactts

/*
#cgo CFLAGS:  -I/System/Library/Frameworks/AudioToolbox.framework/Headers
#cgo LDFLAGS: -framework AudioToolbox
#include <AudioFile.h>
#include <ExtendedAudioFile.h>

extern OSStatus go_audiofile_readproc(void *data, SInt64 inPosition, UInt32 requestCount, void *buffer, UInt32 *actualCount);
extern OSStatus go_audiofile_writeproc(void *data, SInt64 inPosition, UInt32 requestCount, void *buffer, UInt32 *actualCount);
extern SInt64 go_audiofile_getsizeproc(void *data);
*/
import "C"
import "fmt"
import "unsafe"
import "io"
import "reflect"
import "runtime"

//export go_audiofile_getsizeproc
func go_audiofile_getsizeproc(data unsafe.Pointer) C.SInt64 {
	af := (*AudioFile)(data)
	return C.SInt64(af.fileSize)
}

//export go_audiofile_readproc
func go_audiofile_readproc(data unsafe.Pointer, inPosition C.SInt64, requestCount C.UInt32, buffer unsafe.Pointer, actualCount *C.UInt32) C.OSStatus {
	af := (*AudioFile)(data)
	length := int(requestCount)
	hdr := reflect.SliceHeader{
		Data: uintptr(buffer),
		Len:  length,
		Cap:  length,
	}
	bslice := *(*[]byte)(unsafe.Pointer(&hdr))
	n, err := af.target.ReadAt(bslice, int64(inPosition))
	*actualCount = C.UInt32(n)
	if err == io.EOF {
		return C.kAudioFileEndOfFileError
	} else if err != nil {
		return C.kAudioFileUnspecifiedError
	}
	return C.OSStatus(0)
}

//export go_audiofile_writeproc
func go_audiofile_writeproc(data unsafe.Pointer, inPosition C.SInt64, requestCount C.UInt32, buffer unsafe.Pointer, actualCount *C.UInt32) C.OSStatus {
	af := (*AudioFile)(data)
	length := int(requestCount)
	hdr := reflect.SliceHeader{
		Data: uintptr(buffer),
		Len:  length,
		Cap:  length,
	}
	bslice := *(*[]byte)(unsafe.Pointer(&hdr))
	npos := int64(inPosition)

	n, err := af.target.WriteAt(bslice, npos)
	*actualCount = C.UInt32(n)
	if err != nil {
		return C.kAudioFileUnspecifiedError
	}
	if npos+int64(n) > af.fileSize {
		af.fileSize = npos + int64(n)
	}

	return C.OSStatus(0)
}

func osStatToString(t C.OSStatus) string {
	return string([]byte{byte((t >> 24) & 0xFF), byte((t >> 16) & 0xFF), byte((t >> 8) & 0xFF), byte(t & 0xFF)})
}

func osStatus(stat C.OSStatus) error {
	if stat == 0 {
		return nil
	}
	return fmt.Errorf("OSStatus: %v", osStatToString(stat))
}

// ReadWriterAt is a composed interface of a standard io.ReaderAt and io.WriterAt.
type ReadWriterAt interface {
	io.ReaderAt
	io.WriterAt
}

// AudioFile wraps a CoreAudio AudioFile handle and allows handling file operations within Go.
type AudioFile struct {
	id       C.AudioFileID
	target   ReadWriterAt
	fileSize int64
}

// NewOutputWaveFile opens a CoreAudio WAVE file suitable for output to target
//
// rate is the sample rate, numchan is the number of channels in the output, and numbits is the number of bits per channel.
// NOTE: In order to prevent unnecessary copying, the calls to target use buffers that are owned by CoreAudio. This means that
// slices should not be made of buffers that will outlive the call to the WriteAt method.
func NewOutputWaveFile(target ReadWriterAt, rate float64, numchan int, numbits int) (*AudioFile, error) {
	var af = AudioFile{
		target: target,
	}
	bpf := C.UInt32(numbits * numchan / 8)
	var asbd = C.AudioStreamBasicDescription{
		mSampleRate:       C.Float64(rate),
		mFormatID:         C.kAudioFormatLinearPCM,
		mFormatFlags:      C.kAudioFormatFlagIsSignedInteger | C.kAudioFormatFlagIsPacked,
		mBytesPerPacket:   bpf,
		mFramesPerPacket:  1,
		mBytesPerFrame:    bpf,
		mChannelsPerFrame: C.UInt32(numchan),
		mBitsPerChannel:   C.UInt32(numbits),
	}
	stat := C.AudioFileInitializeWithCallbacks(unsafe.Pointer(&af), (*[0]byte)(C.go_audiofile_readproc), (*[0]byte)(C.go_audiofile_writeproc),
		(*[0]byte)(C.go_audiofile_getsizeproc), nil, C.kAudioFileWAVEType, &asbd, 0, &af.id)
	if stat != 0 {
		return nil, osStatus(stat)
	}
	runtime.SetFinalizer(&af, func(af *AudioFile) {
		if af.id != nil {
			C.AudioFileClose(af.id)
		}
	})
	return &af, nil
}

// ExtAudioFile returns a ExtAudioFile that wraps the CoreAudio AudioFile
func (af *AudioFile) ExtAudioFile() (*ExtAudioFile, error) {
	var eaf = ExtAudioFile{af: af}
	stat := C.ExtAudioFileWrapAudioFileID(af.id, 1, &eaf.ceaf)
	if stat != 0 {
		return nil, osStatus(stat)
	}
	runtime.SetFinalizer(&eaf, func(eaf *ExtAudioFile) {
		if eaf.ceaf != nil {
			C.ExtAudioFileDispose(eaf.ceaf)
		}
	})
	return &eaf, nil
}

// Close closes the AudioFile and releases the reference to it.
//
// When used with ExtAudioFile this function must not be called while the ExtAudioFile is still in use.
func (af *AudioFile) Close() error {
	if af.id == nil {
		return nil
	}
	stat := C.AudioFileClose(af.id)
	af.id = nil
	return osStatus(stat)
}

// ExtAudioFile wraps a CoreAudio ExtAudioFile handle.
type ExtAudioFile struct {
	ceaf C.ExtAudioFileRef
	af   *AudioFile
}

// Close closes the ExtAudioFile and releases the reference to it.
func (eaf *ExtAudioFile) Close() error {
	if eaf.ceaf == nil {
		return nil
	}
	stat := C.ExtAudioFileDispose(eaf.ceaf)
	eaf.ceaf = nil
	eaf.af = nil
	return osStatus(stat)
}
