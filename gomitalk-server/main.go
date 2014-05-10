package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"bitbucket.org/ww/goautoneg"
	"github.com/jkl1337/mactts"
	"encoding/binary"
	"crypto/md5"
	"encoding/hex"
)

const (
	jsonMIMEType = "application/json; charset=utf-8"
	textMIMEType = "text/plain; charset=utf-8"
)

var (
	httpAddr = flag.String("http", ":8080", "Listen for HTTP connections on this address.")
	useEtag = flag.Bool("etag", true, "Produce Etags for equivalent utterances.")
)

// stripPort removes the port specification from an address
func stripPort(s string) string {
	if h, _, err := net.SplitHostPort(s); err == nil {
		s = h
	}
	return s
}

type httpError struct {
	status int   // HTTP status code
	err    error // optional reason for HTTP error
}

func (err *httpError) Error() string {
	if err.err != nil {
		return fmt.Sprintf("status %d, reason %s", err.status, err.err.Error())
	}
	return fmt.Sprintf("Status %d", err.status)
}

type httpErrFunc func(resp http.ResponseWriter, req *http.Request, status int, err error)

func logError(req *http.Request, err error, rv interface{}) {
	if err != nil {
		var buf bytes.Buffer
		fmt.Fprintf(&buf, "Error %s: %v\n", req.URL, err)
		if rv != nil {
			fmt.Fprintln(&buf, rv)
			buf.Write(debug.Stack())
		}
		log.Print(buf.String())
	}
}

func runHandler(resp http.ResponseWriter, req *http.Request,
	fn func(resp http.ResponseWriter, req *http.Request) error, errfn httpErrFunc) {

	defer func() {
		if rv := recover(); rv != nil {
			err := errors.New("handler panic")
			logError(req, err, rv)
			errfn(resp, req, http.StatusInternalServerError, err)
		}
	}()

	if s := req.Header.Get("X-Real-Ip"); s != "" && stripPort(req.RemoteAddr) == "127.0.0.1" {
		req.RemoteAddr = s
	}

	req.Body = http.MaxBytesReader(resp, req.Body, 131072)
	req.ParseForm()
	var rb ResponseBuffer
	err := fn(&rb, req)
	if err == nil {
		// nginx Etag handling breaks if Last-Modified is not set
		if rb.Header()["Last-Modified"] == nil {
			rb.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		}
		// For nginx proxy cache, this allows the support of byte ranges. Not sure how to get around this on the nginx side.
		rb.Header().Set("Accept-Ranges", "bytes")
		rb.WriteTo(resp)
	} else if e, ok := err.(*httpError); ok {
		if e.status >= 500 {
			logError(req, err, nil)
		}
		errfn(resp, req, e.status, e.err)
	} else {
		logError(req, err, nil)
		errfn(resp, req, http.StatusInternalServerError, err)
	}
}

func handleJSONError(resp http.ResponseWriter, req *http.Request, status int, err error) {
	var data struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if status <= 500 {
		data.Error.Message = err.Error()
	} else {
		data.Error.Message = http.StatusText(status)
	}
	resp.Header().Set("Content-Type", jsonMIMEType)
	resp.WriteHeader(status)
	json.NewEncoder(resp).Encode(&data)
}

type apiHandler func(resp http.ResponseWriter, req *http.Request) error

func (h apiHandler) ServeHTTP(resp http.ResponseWriter, req *http.Request) {
	runHandler(resp, req, h, handleJSONError)
}

// checkEtagDone returns true if we are certain that the request can be signaled as StatusNotModified.
func checkEtagDone(req *http.Request, etag string) bool {
	if inm := req.Header.Get("If-None-Match"); inm != "" {
		if etag == "" {
			return false
		}
		if inm == etag || inm == "*" {
			return true
		}
	}
	return false
}

func speechHandler(resp http.ResponseWriter, req *http.Request) error {
	msg := req.FormValue("text")
	if msg == "" {
		return &httpError{status: http.StatusBadRequest, err: errors.New("missing `text` parameter")}
	}

	// name match is highest priority, followed by gender/locale match, and then fallback
	var voiceSpec *mactts.VoiceSpec
	voiceName := req.FormValue("voice")
	if voiceName != "" {
		v := voices.FindByName(voiceName)
		if v == nil {
			return &httpError{status: http.StatusNotFound, err: fmt.Errorf("voice `%s` not found", voiceName)}
		}
		voiceSpec = v.Spec()
	} else {
		var gender mactts.Gender
		switch req.FormValue("gender") {
		case "":
			gender = mactts.GenderNil
		case "male":
			gender = mactts.GenderMale
		case "neuter":
			gender = mactts.GenderNeuter
		case "female":
			gender = mactts.GenderFemale
		default:
			return &httpError{status: http.StatusBadRequest, err: fmt.Errorf("invalid `gender` parameter")}
		}
		locale := req.FormValue("lang")
		if gender != mactts.GenderNil || locale != "" {
			v := voices.Match(gender, locale)
			if v == nil {
				return &httpError{status: http.StatusNotFound, err: errors.New("cannot find voice with specified gender and/or language")}
			}
			voiceSpec = v.Spec()
		}
	}

	// finally if no matcher hits, try to load a universally available default
	if voiceSpec == nil {
		v := voices.FindByName("Fred")
		if v == nil {
			return &httpError{status: http.StatusNotFound, err: errors.New("unable to find suitable voice")}
		}
		voiceSpec = v.Spec()
	}

	sampleRate := 22050
	switch req.FormValue("samplerate") {
	case "8000":
		sampleRate = 8000
	case "11025":
		sampleRate = 11025
	case "16000":
		sampleRate = 16000
	case "32000":
		sampleRate = 32000
	case "44100":
		sampleRate = 44100
	case "48000":
		sampleRate = 48000
	}

	if req.Method == "POST" {
		attachmentName := req.FormValue("attachment")
		if attachmentName != "" {
			resp.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachmentName))
		}
	}
	f := resp.(*ResponseBuffer)

	acceptMimeType := req.FormValue("type")
	if acceptMimeType == "" {
		acceptMimeType = req.Header.Get("Accept")
	}
	acceptType := goautoneg.Negotiate(acceptMimeType, []string{
		"audio/wave", "audio/wav", "audio/x-wav", "audio/vnd.wav",
		"audio/mp4",
	})

	responseType := "audio/wav"
	newFileFunc := mactts.NewOutputWAVEFile
	if acceptType == "audio/mp4" {
		responseType = "audio/mp4"
		newFileFunc = mactts.NewOutputAACFile
	}

	resp.Header().Set("Content-Type", responseType)

	if *useEtag {
		// compute the Etag
		etagBuf := make([]byte, 4, 24+len(msg))
		binary.BigEndian.PutUint32(etagBuf, uint32(sampleRate))
		vsb, _ := voiceSpec.MarshalBinary()
		etagBuf = append(etagBuf, vsb...)
		etagBuf = append(etagBuf, responseType...)
		etagBuf = append(etagBuf, msg...)
		etagSum := md5.New()
		etagSum.Write(etagBuf)

		etag := hex.EncodeToString(etagSum.Sum(make([]byte, 0, 16)))
		resp.Header().Set("Etag", etag)

		if done := checkEtagDone(req, etag); done {
			resp.WriteHeader(http.StatusNotModified)
			return nil
		}
	}

	af, err := newFileFunc(f, float64(sampleRate), 1, 16)
	if err != nil {
		return err
	}
	defer af.Close()

	eaf, err := af.ExtAudioFile()
	if err != nil {
		return err
	}
	defer eaf.Close()

	sc, err := mactts.NewChannel(voiceSpec)
	if err != nil {
		return err
	}
	defer sc.Close()

	if err = sc.SetExtAudioFile(eaf); err != nil {
		return err
	}

	done := make(chan int)
	err = sc.SetDone(func() {
		done <- 1
		close(done)
	})
	if err != nil {
		return err
	}

	if err = sc.SpeakString(msg); err != nil {
		return nil
	}
	select {
	case <-done:
	case <-time.After(1 * time.Minute):
		return errors.New("timed out synthesizing speech")
	}
	return nil
}

// Voice is a representation of system voice metadata.
type Voice struct {
	spec       mactts.VoiceSpec
	gender     mactts.Gender
	Name       string `json:"name"`
	Locale     string `json:"locale,omitempty"`
	Gender     string `json:"gender"`
	Age        int    `json:"age"`
	Identifier string `json:"id,omitempty"`
}

// Spec returns the system VoiceSpec.
func (v *Voice) Spec() *mactts.VoiceSpec {
	return &v.spec
}

// VoiceCollection is a JSON marshalable and searchable container of system voices.
type VoiceCollection struct {
	voices      []Voice
	voiceByName map[string]*Voice
	json        []byte
}

func (vc *VoiceCollection) MarshalJSON() ([]byte, error) {
	var err error
	if len(vc.json) == 0 {
		v := struct {
			Voices []Voice `json:"voices"`
		}{Voices: vc.voices}
		vc.json, err = json.Marshal(&v)
	}
	return vc.json, err
}

// Match finds a matching voice for a gender and a locale. locale may be empty, in which
// case it is treated as en-US. The gender may be the value GenderNone, which means that gender is ignored.
// Match will return nil if it cannot match the parameters specified.
func (vc *VoiceCollection) Match(gender mactts.Gender, locale string) *Voice {
	if locale == "" {
		locale = "en_US"
	}
	for _, v := range vc.voices {
		if (gender == mactts.GenderNil || gender == v.gender) && (locale == v.Locale) {
			return &v
		}
	}
	return nil
}

// FindByName finds a voice in the collection with the given system name.
func (vc *VoiceCollection) FindByName(name string) *Voice {
	return vc.voiceByName[name]
}

var voices VoiceCollection

func loadVoices() error {
	n, err := mactts.NumVoices()
	if err != nil {
		return err
	}
	vs := make([]Voice, n)
	vsm := make(map[string]*Voice)
	for i := 0; i < n; i++ {
		v, err := mactts.GetVoice(i + 1)
		if err != nil {
			return err
		}
		desc, err := v.Description()
		if err != nil {
			return nil
		}

		var locale, identifier string
		if attr, err := v.Attributes(); err == nil {
			locale = attr.LocaleIdentifier()
			identifier = attr.Identifier()
		}
		name := desc.Name()
		vs[i] = Voice{
			spec:       *v,
			gender:     desc.Gender(),
			Name:       name,
			Locale:     locale,
			Gender:     desc.Gender().String(),
			Age:        desc.Age(),
			Identifier: identifier,
		}
		vsm[name] = &vs[i]
	}
	voices.voices = vs
	voices.voiceByName = vsm
	return nil
}

func voicesHandler(resp http.ResponseWriter, req *http.Request) error {
	resp.Header().Set("Content-Type", jsonMIMEType)
	data, err := voices.MarshalJSON()
	if err != nil {
		return err
	}
	_, err = resp.Write(data)
	return err
}

func main() {
	flag.Parse()
	log.Printf("Starting server, os.Args=%s", strings.Join(os.Args, " "))

	if err := loadVoices(); err != nil {
		log.Fatal(err)
	}

	http.Handle("/voices", apiHandler(voicesHandler))
	http.Handle("/say", apiHandler(speechHandler))
	log.Fatal(http.ListenAndServe(*httpAddr, nil))
}
