mactts
======

mactts is a small Golang wrapper for the Mac OSX Speech Synthesis Manager Carbon API.
This was originally created for the production of TTS speech server demo. While newer versions of Mac OSX ship with the typical Nuance Vocalizer® voices available from many places, there is no matching the utility of the original MacInTalk Pro voices. And when you need them and don't have a real Mac handy, what is one to do?

Usage
=====

The server command is called gomitalk-server and it is go gettable:
```
go get github.com/jkl1337/mactts/gomitalk-server
```

The command gomitalk-server can now be run from the command line. The default port is 8080.

Now go to (http://localhost:8080/speech?text=Hello+World&voice=Hysterical) in your browser. You should get a 22,050 Hz 16-bit WAVE file sent to your browser.

An example

Server API
==========
The server API supports two endpoints, one for speech and one for information about the voices available:

```
GET /speech{?text,lang,gender,samplerate,type,attachment}
POST /speech (application/x-www-form-urlencoded or multipart/form-data)

text : The UTF-8 encoded message text to synthesize as speech.
lang : A locale identifier {language_territory} such as en-US, en-GB that is used to match against the
       available voices. If no match is found, a 404 is returned.
       Currently, only the language-territory format is allowed.
gender : A gender name {male,female,neuter} used to match against available voices. If this is
         specified and no match is found, a 404 is returned.
samplerate : One of (8000, 11025, 16000, 32000, 44100, 48000). If no match is found, 22050 is used.
type : The preferred MIME type of the audio. Either audio/wav or audio/mp4. Some equivalent variants of
       these are allowed. This is provided because most browsers such as Chrome and Firefox do not use the
       type attribute of the <audio> element to set the Accept header in such a way that the preferred
       audio type is retrieved.
attachment: A filename that is used to set the Content-Disposition header.


GET /voices

Returns a JSON object with names, languages, and genders of available voices on the system.
```


TODO
====
- Web frontend API
- Provide MP4 and Vorbis output so the audio tag can be used on all browsers with compression.
