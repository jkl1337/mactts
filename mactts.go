package mactts

/*
#cgo CFLAGS:  -I/System/Library/Frameworks/ApplicationServices.framework/Versions/A/Frameworks/SpeechSynthesis.framework/Versions/A/Headers/
#cgo LDFLAGS: -framework ApplicationServices
#include <SpeechSynthesis.h>

extern void go_speechdone_cb(SpeechChannel csc, long refcon);
*/
import "C"
import "runtime"
import "errors"
import "fmt"
import "unsafe"

//export go_speechdone_cb
func go_speechdone_cb(csc C.SpeechChannel, refcon C.long) {
	c := (*Channel)(unsafe.Pointer(uintptr(refcon)))
	if c.done != nil {
		c.done()
	}
}

// VoiceSpec uniquely identifies a speech synthesizer voice on the system.
type VoiceSpec struct {
	Creator, Id uint
}

// Channel is a independent channel resource for speech synthesis within the synthesizer.
//
// There is no predefined limit on the number of speech channels an application can create. However, system constraints on
// available RAM, processor loading, and number of available sound channels limit the number of speech channels actually possible.
type Channel struct {
	csc  C.SpeechChannel
	done func()
}

var osErrorMap = map[int]error{
	-240: errors.New("Could not find the specified speech synthesizer"),
	-241: errors.New("Could not open another speech synthesizer channel"),
	-242: errors.New("Speech synthesizer is still busy speaking"),
	-243: errors.New("Output buffer is too small to hold result"),
	-244: errors.New("Voice resource not found"),
	-245: errors.New("Specified voice cannot be used with synthesizer"),
	-246: errors.New("Pronunciation dictionary format error"),
	-247: errors.New("Raw phoneme text contains invalid characters"),
}

func osTypeToString(t C.OSType) string {
	return string([]byte{byte((t >> 24) & 0xFF), byte((t >> 16) & 0xFF), byte((t >> 8) & 0xFF), byte(t & 0xFF)})
}

func osError(oserr C.OSErr) error {
	if oserr == 0 {
		return nil
	}
	e := osErrorMap[int(oserr)]
	if e == nil {
		e = fmt.Errorf("Unknown OSErr: %v", int(oserr))
	}
	return e
}

// cfstring efficiently creates a CFString from a Go String
func cfstring(s string) C.CFStringRef {
	n := C.CFIndex(len(s))
	return C.CFStringCreateWithBytes(nil, *(**C.UInt8)(unsafe.Pointer(&s)), n, C.kCFStringEncodingUTF8, 0)
}

// GetVoice returns a voice specification for an index.
//
// The maximum value of n can be determined by calling NumVoices. If the value of n is invalid (too large or below 1),
// GetVoice will return nil.
func GetVoice(n int) (*VoiceSpec, error) {
	var cvs C.VoiceSpec
	oserr := C.GetIndVoice(C.SInt16(n), &cvs)
	if oserr == C.voiceNotFound {
		return nil, nil
	} else if oserr != 0 {
		return nil, osError(oserr)
	}
	return &VoiceSpec{uint(cvs.creator), uint(cvs.id)}, nil
}

// NumVoices determines how many voices are available on the system.
func NumVoices() (int, error) {
	var cn C.SInt16
	oserr := C.CountVoices(&cn)
	if oserr != 0 {
		return 0, osError(oserr)
	}
	return int(cn), nil
}

// Gender is used to indicate the gender of the individual represented by a voice.
type Gender int

const (
	GenderNeuter Gender = C.kNeuter
	GenderFemale        = C.kFemale
	GenderMale          = C.kMale
)

func (g Gender) String() string {
	switch g {
	case GenderNeuter:
		return "Neuter"
	case GenderFemale:
		return "Female"
	case GenderMale:
		return "Male"
	}
	return "(Invalid)"
}

// VoiceDescription provides metadata for a speech synthesizer voice.
type VoiceDescription struct {
	cvd C.VoiceDescription
}

// VoiceSpec provides the unique voice specifier for this voice description.
func (vd VoiceDescription) VoiceSpec() VoiceSpec {
	return VoiceSpec{uint(vd.cvd.voice.creator), uint(vd.cvd.voice.id)}
}

// Version is the version number of the voice.
func (vd VoiceDescription) Version() int {
	return int(vd.cvd.version)
}

// Name is the short name of the voice as listed in the Speech Manager.
func (vd VoiceDescription) Name() string {
	cvd := vd.cvd
	return C.GoStringN((*C.char)(unsafe.Pointer(&cvd.name[1])), C.int(cvd.name[0]))
}

// Comment is additional text information about the voice. Some synthesizers use this field to store an example phrase that can be spoken.
func (vd VoiceDescription) Comment() string {
	cvd := vd.cvd
	return C.GoStringN((*C.char)(unsafe.Pointer(&cvd.comment[1])), C.int(cvd.comment[0]))
}

// Gender is the gender of the individual represented by the voice.
func (vd VoiceDescription) Gender() Gender {
	return Gender(vd.cvd.gender)
}

// Age is the approximate age in years of the individual represented by the voice.
func (vd VoiceDescription) Age() int {
	return int(vd.cvd.age)
}

// Description provides access to the metadata for the voice.
func (vs *VoiceSpec) Description() (vd VoiceDescription, err error) {
	var cvs C.VoiceSpec
	oserr := C.MakeVoiceSpec(C.OSType(vs.Creator), C.OSType(vs.Id), &cvs)
	if oserr != 0 {
		err = osError(oserr)
		return
	}
	oserr = C.GetVoiceDescription(&cvs, &vd.cvd, C.long(unsafe.Sizeof(vd.cvd)))
	if oserr != 0 {
		err = osError(oserr)
		return
	}
	return
}

func disposeSpeechChannel(c *Channel) {
	if c.csc != nil {
		C.DisposeSpeechChannel(c.csc)
	}
}

// NewChannel creates a speech synthesizer speech channel with option voice specification. If no voice is provided, the system voice is used.
func NewChannel(voice *VoiceSpec) (*Channel, error) {
	var c Channel

	var vsp *C.VoiceSpec
	if voice != nil {
		var vs C.VoiceSpec
		oserr := C.MakeVoiceSpec(C.OSType(voice.Creator), C.OSType(voice.Id), &vs)
		if oserr != 0 {
			return nil, osError(oserr)
		}
		vsp = &vs
	}

	oserr := C.NewSpeechChannel(vsp, &c.csc)
	if oserr != 0 {
		return nil, osError(oserr)
	}

	rcp := &c
	cfrc := C.CFNumberCreate(nil, C.kCFNumberLongType, unsafe.Pointer(&rcp))
	defer C.CFRelease(C.CFTypeRef(cfrc))
	oserr = C.SetSpeechProperty(c.csc, C.kSpeechRefConProperty, C.CFTypeRef(cfrc))
	if oserr != 0 {
		disposeSpeechChannel(&c)
		return nil, osError(oserr)
	}

	runtime.SetFinalizer(&c, disposeSpeechChannel)
	return &c, nil
}

// SetDone sets a synthesis completion callback function for the speech channel.
func (c *Channel) SetDone(done func()) error {
	cbp := C.go_speechdone_cb
	cfdc := C.CFNumberCreate(nil, C.kCFNumberLongType, unsafe.Pointer(&cbp))
	defer C.CFRelease(C.CFTypeRef(cfdc))
	oserr := C.SetSpeechProperty(c.csc, C.kSpeechSpeechDoneCallBack, C.CFTypeRef(cfdc))
	if oserr != 0 {
		return osError(oserr)
	}
	c.done = done
	return nil
}

// SpeakString asynchronously queues the string for synthesis by the channel.
func (c *Channel) SpeakString(s string) error {
	cfs := cfstring(s)
	defer C.CFRelease(C.CFTypeRef(cfs))
	return osError(C.SpeakCFString(c.csc, cfs, nil))
}

// SetRate sets the speech rate in words-per-minute.
//
// SetRate adjusts the rate of the speech channel to the rate specified by the rate parameter. As a general rule, speaking rates
// range from around 150 words per minute to around 220 words per minute. It is important to keep in mind, however, that users will
// differ greatly in their ability to understand synthesized speech at a particular rate based upon their level of experience
// listening to the voice and their ability to anticipate the types of utterances they will encounter.
func (c *Channel) SetRate(rate int) error {
	return osError(C.SetSpeechRate(c.csc, C.Fixed(rate<<16)))
}

// SetPitchBase sets the pitch of the speech with frequency mapped as a MIDI note number.
//
// SetPitchBase changes the current speech pitch on the speech channel to the pitch specified by the pitch parameter. Typical voice
// frequencies range from around 90 Hz for a low-pitched male voice to perhaps 300 Hz for a high-pitched child's voice. These
// frequencies correspond to approximate pitch values in the ranges of 30.000 to 40.000 and 55.000 to 65.000, respectively.
// Although fixed-point values allow you to specify a wide range of pitches, not all synthesizers will support the full range of
// pitches. If your application specifies a pitch that a synthesizer cannot handle, it may adjust the pitch to fit within
// an acceptable range.
func (c *Channel) SetPitchBase(pitch float64) error {
	return osError(C.SetSpeechPitch(c.csc, C.Fixed(pitch*65536)))
}

// SetPitchMod sets the pitch modulation of the speech with frequency mapped as a MIDI note number.
//
// Pitch modulation is valid within the range of 0.000 to 127.000, corresponding to MIDI note values, where 60.000 is equal
// to middle C on a piano scale. The most useful speech pitches fall in the range of 40.000 to 55.000. A pitch modulation value
// of 0.000 corresponds to a monotone in which all speech is generated at the frequency corresponding to the speech pitch. Given
// a speech pitch value of 46.000, a pitch modulation of 2.000 would mean that the widest possible range of pitches corresponding
// to the actual frequency of generated text would be 44.000 to 48.000.
func (c *Channel) SetPitchMod(mod float64) error {
	cfn := C.CFNumberCreate(nil, C.kCFNumberFloat64Type, unsafe.Pointer(&mod))
	defer C.CFRelease(C.CFTypeRef(cfn))
	return osError(C.SetSpeechProperty(c.csc, C.kSpeechPitchModProperty, C.CFTypeRef(cfn)))
}

// SetVolume sets the speech channel volume.
//
// Speech volumes are expressed in values ranging from 0.0 through 1.0. A value of 0.0 corresponds to silence, and a value of 1.0
// corresponds to the maximum possible volume. Volume units lie on a scale that is linear with amplitude or voltage. A doubling
// of perceived loudness corresponds to a doubling of the volume.
func (c* Channel) SetVolume(volume float64) error {
	cfn := C.CFNumberCreate(nil, C.kCFNumberFloat64Type, unsafe.Pointer(&volume))
	defer C.CFRelease(C.CFTypeRef(cfn))
	return osError(C.SetSpeechProperty(c.csc, C.kSpeechVolumeProperty, C.CFTypeRef(cfn)))
}

// SetExtAudioFile sets the channel's output destination to an extended audio file, or back to the speakers, if eaf is nil.
func (c *Channel) SetExtAudioFile(eaf *ExtAudioFile) error {
	cfn := C.CFNumberCreate(nil, C.kCFNumberLongType, unsafe.Pointer(&eaf.ceaf))
	defer C.CFRelease(C.CFTypeRef(cfn))
	return osError(C.SetSpeechProperty(c.csc, C.kSpeechOutputToExtAudioFileProperty, C.CFTypeRef(cfn)))
}

// Stop terminates speech generation on the channel immediately.
//
// Stop can be called on idle channel without ill effect.
func (c *Channel) Stop() error {
	return osError(C.StopSpeech(c.csc))
}

// Close closes the synthesizer speech channel and releases all internal resources.
func (c *Channel) Close() error {
	if c.csc == nil {
		return nil
	}
	oserr := C.DisposeSpeechChannel(c.csc)
	c.csc = nil
	return osError(oserr)
}

// Busy indicates whether any speech channels are currently processing speech.
func Busy() bool {
	return C.SpeechBusy() != 0
}
