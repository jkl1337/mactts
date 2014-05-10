package mactts

/*
#cgo CFLAGS:  -I/System/Library/Frameworks/ApplicationServices.framework/Versions/A/Frameworks/SpeechSynthesis.framework/Versions/A/Headers/
#cgo LDFLAGS: -framework ApplicationServices
#include <SpeechSynthesis.h>

enum {
  soVoiceAttributes = 'attr'
};

extern CFStringRef kSpeechVoiceName;
extern CFStringRef kSpeechVoiceIdentifier;
extern CFStringRef kSpeechVoiceDemoText;
extern CFStringRef kSpeechVoiceAge;
extern CFStringRef kSpeechVoiceGender;
extern CFStringRef kSpeechVoiceLocaleIdentifier;

extern void go_speechdone_cb(SpeechChannel csc, long refcon);

// cfstring_utf8_length returns the number of characters successfully converted to UTF-8 and
// the bytes required to store them.
static inline CFIndex cfstring_utf8_length(CFStringRef str, CFIndex *need) {
  CFIndex n, usedBufLen;
  CFRange rng = CFRangeMake(0, CFStringGetLength(str));

  return CFStringGetBytes(str, rng, kCFStringEncodingUTF8, 0, 0, NULL, 0, need);
}

static inline OSErr mactts_set_property_float64(SpeechChannel chan, CFStringRef prop, double n) {
  CFNumberRef cfn = CFNumberCreate(NULL, kCFNumberFloat64Type, &n);
  OSErr ret = SetSpeechProperty(chan, prop, cfn);
  CFRelease(cfn);
  return ret;
}

static inline OSErr mactts_set_property_ptr(SpeechChannel chan, CFStringRef prop, void *p) {
  CFNumberRef cfn = CFNumberCreate(NULL, kCFNumberLongType, &p);
  OSErr ret = SetSpeechProperty(chan, prop, cfn);
  CFRelease(cfn);
  return ret;
}

*/
import "C"
import "runtime"
import "errors"
import "fmt"
import "unsafe"
import "reflect"

//export go_speechdone_cb
func go_speechdone_cb(csc C.SpeechChannel, refcon C.long) {
	c := (*Channel)(unsafe.Pointer(uintptr(refcon)))
	if c.done != nil {
		c.done()
	}
}

// VoiceSpec uniquely identifies a speech synthesizer voice on the system.
type VoiceSpec C.VoiceSpec

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

// cfstring efficiently creates a CFString from a Go String.
func cfstring(s string) C.CFStringRef {
	n := C.CFIndex(len(s))
	return C.CFStringCreateWithBytes(nil, *(**C.UInt8)(unsafe.Pointer(&s)), n, C.kCFStringEncodingUTF8, 0)
}

// cfstringGo creates a Go string for a CoreFoundation string using the CoreFoundation UTF-8 converter.
// For short strings this is an efficiency nightmare! In this package this function is not currently used
// in any critical path.
func cfstringGo(cfs C.CFStringRef) string {
	var usedBufLen C.CFIndex
	n := C.cfstring_utf8_length(cfs, &usedBufLen)
	if n <= 0 {
		return ""
	}
	rng := C.CFRange{location: C.CFIndex(0), length: n}
	buf := make([]byte, int(usedBufLen))

	bufp := unsafe.Pointer(&buf[0])
	C.CFStringGetBytes(cfs, rng, C.kCFStringEncodingUTF8, 0, 0, (*C.UInt8)(bufp), C.CFIndex(len(buf)), &usedBufLen)

	sh := &reflect.StringHeader{
		Data: uintptr(bufp),
		Len:  int(usedBufLen),
	}
	return *(*string)(unsafe.Pointer(sh))
}

// GetVoice returns a voice specification for an index.
//
// The maximum value of n can be determined by calling NumVoices. If the value of n is invalid (too large or below 1),
// GetVoice will return nil.
func GetVoice(n int) (*VoiceSpec, error) {
	var vs VoiceSpec
	oserr := C.GetIndVoice(C.SInt16(n), (*C.VoiceSpec)(&vs))
	if oserr == C.voiceNotFound {
		return nil, nil
	} else if oserr != 0 {
		return nil, osError(oserr)
	}
	return &vs, nil
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
	GenderNil    Gender = -1
	GenderNeuter        = C.kNeuter
	GenderFemale        = C.kFemale
	GenderMale          = C.kMale
)

func (g Gender) String() string {
	switch g {
	case GenderNil:
		return "nil"
	case GenderNeuter:
		return "neuter"
	case GenderFemale:
		return "female"
	case GenderMale:
		return "male"
	}
	return "(invalid)"
}

// VoiceDescription provides metadata for a speech synthesizer voice.
type VoiceDescription C.VoiceDescription

// VoiceSpec provides the unique voice specifier for this voice description.
func (vd *VoiceDescription) VoiceSpec() (vs VoiceSpec) {
	C.MakeVoiceSpec(vd.voice.creator, vd.voice.id, (*C.VoiceSpec)(&vs))
	return
}

// Version is the version number of the voice.
func (vd *VoiceDescription) Version() int {
	return int(vd.version)
}

// Name is the short name of the voice as listed in the Speech Manager.
func (vd *VoiceDescription) Name() string {
	return C.GoStringN((*C.char)(unsafe.Pointer(&vd.name[1])), C.int(vd.name[0]))
}

// Comment is additional text information about the voice. Some synthesizers use this field to store an example phrase that can be spoken.
func (vd *VoiceDescription) Comment() string {
	return C.GoStringN((*C.char)(unsafe.Pointer(&vd.comment[1])), C.int(vd.comment[0]))
}

// Gender is the gender of the individual represented by the voice.
func (vd *VoiceDescription) Gender() Gender {
	return Gender(vd.gender)
}

// Age is the approximate age in years of the individual represented by the voice.
func (vd *VoiceDescription) Age() int {
	return int(vd.age)
}

// Description provides access to the metadata for the voice.
func (vs VoiceSpec) Description() (vd VoiceDescription, err error) {
	oserr := C.GetVoiceDescription((*C.VoiceSpec)(&vs), (*C.VoiceDescription)(&vd), C.long(unsafe.Sizeof(vd)))
	if oserr != 0 {
		err = osError(oserr)
		return
	}
	return
}

// VoiceAttributes wraps a CoreFoundation dictionary that contains additional metadata about a voice
// The information contained that is not avaiable from VoiceDescription includes the system Identifier,
// and the LocaleIdentifier.
type VoiceAttributes struct {
	cfd C.CFDictionaryRef
}

func (d VoiceAttributes) get(k C.CFStringRef) (s string) {
	cs := C.CFDictionaryGetValue(d.cfd, unsafe.Pointer(k))
	if cs != nil {
		s = cfstringGo(C.CFStringRef(cs))
	}
	return
}

// Name is the short name of the voice as listed in the Speech Manager.
func (d VoiceAttributes) Name() string {
	return d.get(C.kSpeechVoiceName)
}

// Identifier provides a unique string identifying the voice.
func (d VoiceAttributes) Identifier() string {
	return d.get(C.kSpeechVoiceIdentifier)
}

// LocaleIdentifier is the language of the voice.
func (d VoiceAttributes) LocaleIdentifier() string {
	return d.get(C.kSpeechVoiceLocaleIdentifier)
}

// DemoText is additional text information about the voice. Some synthesizers use this field to store an example phrase that can be spoken.
func (d VoiceAttributes) DemoText() string {
	return d.get(C.kSpeechVoiceDemoText)
}

// Attributes provides metadata about the voice.
// The attributes for a voice are described in the documentation for [NSSpeechSynthesizer attributesForVoice].
// This functionality is undocumented in the Carbon Speech Synthesis Manager.
func (vs VoiceSpec) Attributes() (VoiceAttributes, error) {
	var va VoiceAttributes
	oserr := C.GetVoiceInfo((*C.VoiceSpec)(&vs), C.soVoiceAttributes, unsafe.Pointer(&va.cfd))
	if oserr != 0 {
		return va, osError(oserr)
	}
	runtime.SetFinalizer(&va, func(va *VoiceAttributes) {
		C.CFRelease(C.CFTypeRef(va.cfd))
	})
	return va, nil
}

func disposeSpeechChannel(c *Channel) {
	if c.csc != nil {
		C.DisposeSpeechChannel(c.csc)
	}
}

// NewChannel creates a speech synthesizer speech channel with option voice specification. If no voice is provided, the system voice is used.
func NewChannel(voice *VoiceSpec) (*Channel, error) {
	var c Channel

	oserr := C.NewSpeechChannel((*C.VoiceSpec)(voice), &c.csc)
	if oserr != 0 {
		return nil, osError(oserr)
	}

	refCon := unsafe.Pointer(&c)
	oserr = C.mactts_set_property_ptr(c.csc, C.kSpeechRefConProperty, refCon)
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
	oserr := C.mactts_set_property_ptr(c.csc, C.kSpeechSpeechDoneCallBack, cbp)
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
	return osError(C.mactts_set_property_float64(c.csc, C.kSpeechPitchModProperty, C.double(mod)))
}

// SetVolume sets the speech channel volume.
//
// Speech volumes are expressed in values ranging from 0.0 through 1.0. A value of 0.0 corresponds to silence, and a value of 1.0
// corresponds to the maximum possible volume. Volume units lie on a scale that is linear with amplitude or voltage. A doubling
// of perceived loudness corresponds to a doubling of the volume.
func (c *Channel) SetVolume(volume float64) error {
	return osError(C.mactts_set_property_float64(c.csc, C.kSpeechVolumeProperty, C.double(volume)))
}

// SetExtAudioFile sets the channel's output destination to an extended audio file, or back to the speakers, if eaf is nil.
func (c *Channel) SetExtAudioFile(eaf *ExtAudioFile) error {
	var cref unsafe.Pointer
	if eaf != nil {
		cref = unsafe.Pointer(eaf.ceaf)
	}
	return osError(C.mactts_set_property_ptr(c.csc, C.kSpeechOutputToExtAudioFileProperty, cref))
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
	runtime.SetFinalizer(c, nil)
	return osError(oserr)
}

// Busy indicates whether any speech channels are currently processing speech.
func Busy() bool {
	return C.SpeechBusy() != 0
}
