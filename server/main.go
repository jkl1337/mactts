package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"github.com/jkl1337/mactts"
	"log"
	"net"
	"net/http"
	"os"
	"runtime/debug"
	"strings"
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

func speechHandler(resp http.ResponseWriter, req *http.Request) error {
	msg := req.URL.Query().Get("text")
	if msg == "" {
		return &httpError{status: http.StatusBadRequest, err: errors.New("missing `text` parameter")}
	}

	var voice *mactts.VoiceSpec
	voiceName := req.URL.Query().Get("voice")
	if voiceName != "" {
		v := voiceByName[voiceName]
		if v != nil {
			voice = &v.spec
		}
	}

	sc, err := mactts.NewChannel(voice)
	if err != nil {
		return err
	}
	defer sc.Close()

	f := resp.(*ResponseBuffer)
	af, err := mactts.NewOutputWaveFile(f, 16000.0, 1, 16)
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
	<-done
	sc.Close()

	resp.Header().Set("Content-Type", "audio/wave")
	return nil
}

type Voice struct {
	spec mactts.VoiceSpec
	Name string `json:"name"`
	Gender string `json:"gender"`
	Age int `json:"age"`
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
		v, err := mactts.GetVoice(i+1)
		if err != nil {
			return err
		}
		desc, err := v.Description()
		if err != nil {
			return nil
		}
		name := desc.Name()
		vs[i] = Voice{
			spec: *v,
			Name: name,
			Gender: desc.Gender().String(),
			Age: desc.Age(),
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
	}{ Voices: voices }
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
	http.Handle("/speech", apiHandler(speechHandler))
	log.Fatal(http.ListenAndServe(*httpAddr, nil))
}
