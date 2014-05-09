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
)

const (
	jsonMIMEType = "application/json; charset=utf-8"
	textMIMEType = "text/plain; charset=utf-8"
)

var (
	httpAddr = flag.String("http", ":8080", "Listen for HTTP connections on this address.")
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
		rb.CopyHeaders(resp)
		http.ServeContent(resp, req, "", time.Time{}, &rb)
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

func speechHandler(resp http.ResponseWriter, req *http.Request) error {
	msg := req.FormValue("text")
	if msg == "" {
		return &httpError{status: http.StatusBadRequest, err: errors.New("missing `text` parameter")}
	}

	var voice *mactts.VoiceSpec
	voiceName := req.FormValue("voice")
	if voiceName != "" {
		v := voiceByName[voiceName]
		if v != nil {
			voice = &v.spec
		}
	}

	sampleRate := 22050.0
	switch req.FormValue("samplerate") {
	case "8000":
		sampleRate = 8000.0
	case "11025":
		sampleRate = 11025.0
	case "16000":
		sampleRate = 16000.0
	case "32000":
		sampleRate = 32000.0
	case "44100":
		sampleRate = 44100.0
	case "48000":
		sampleRate = 48000.0
	}

	if req.Method == "POST" {
		attachmentName := req.FormValue("attachment")
		if attachmentName != "" {
			resp.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"%s\"", attachmentName))
		}
	}

	sc, err := mactts.NewChannel(voice)
	if err != nil {
		return err
	}
	defer sc.Close()

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

	af, err := newFileFunc(f, sampleRate, 1, 16)
	if err != nil {
		return err
	}
	defer af.Close()

	eaf, err := af.ExtAudioFile()
	if err != nil {
		return err
	}
	defer eaf.Close()

	if err = sc.SetExtAudioFile(eaf); err != nil {
		return err
	}
	defer sc.SetExtAudioFile(nil)

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
		sc.Close()
		return errors.New("timed out synthesizing speech")
	}

	return nil
}

type Voice struct {
	spec   mactts.VoiceSpec
	Name   string `json:"name"`
	Locale string `json:"locale,omitempty"`
	Gender string `json:"gender"`
	Age    int    `json:"age"`
}

var voices []Voice
var voiceByName map[string]*Voice

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
		var locale string
		if attr, err := v.Attributes(); err == nil {
			locale = attr.LocaleIdentifier()
		}
		name := desc.Name()
		vs[i] = Voice{
			spec:   *v,
			Name:   name,
			Locale: locale,
			Gender: desc.Gender().String(),
			Age:    desc.Age(),
		}
		vsm[name] = &vs[i]
	}
	voices = vs
	voiceByName = vsm
	return nil
}

func voicesHandler(resp http.ResponseWriter, req *http.Request) error {
	v := struct {
		Voices []Voice `json:"voices"`
	}{Voices: voices}
	resp.Header().Set("Content-Type", jsonMIMEType)
	json.NewEncoder(resp).Encode(&v)
	return nil
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
