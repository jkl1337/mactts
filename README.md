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

Now go to http://localhost:8080/speech?text=Hello+World&voice=Hysterical in your browser. You should get a 22,050 Hz 16-bit WAVE file sent to your browser.

TODO
====
- Implement ETags based on parameters (internal and external) to allow easy use in a caching proxy situation.
- Web frontend API
- Provide MP4 and Vorbis output so the audio tag can be used on all browsers with compression.
