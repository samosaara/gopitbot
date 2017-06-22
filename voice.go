package main

import (
	"encoding/binary"
	"fmt"
	"io"
	"log"
	"net/http"
	"os/exec"
	"strconv"
	"sync"

	"github.com/bwmarrin/discordgo"
	"layeh.com/gopus"
)

var (
	run          *exec.Cmd
	pcmChannel   = make(chan []int16, 2)
	stop         bool
	trackPlaying bool
)

const (
	channels  int = 2
	frameRate int = 48000
	frameSize int = 960
)

var (
	sendpcm bool
	recv    chan *discordgo.Packet
	mu      sync.Mutex
)

const (
	maxBytes int = (frameSize * 2) * 2 // max size of opus data
)

// VoiceSetup MAKE THIS WARNING GO AWAY
func VoiceSetup(dgv *discordgo.VoiceConnection) {
	go SendPCM(dgv, pcmChannel)
}

// PlayVideo plays a goddamn video
func PlayVideo(dgv *discordgo.VoiceConnection, url string) {
	resp, err := http.Get(url)
	if err != nil {
		log.Printf("Http.Get\nerror: %s\ntarget: %s\n", err, url)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		log.Printf("reading answer: non 200 status code received: '%s'", err)
	}

	run = exec.Command("ffmpeg", "-i", "-", "-f", "s16le", "-ar", strconv.Itoa(frameRate), "-ac", strconv.Itoa(channels), "pipe:1")
	run.Stdin = resp.Body
	stdout, err := run.StdoutPipe()
	if err != nil {
		fmt.Println("StdoutPipe Error:", err)
		return
	}

	err = run.Start()
	if err != nil {
		fmt.Println("RunStart Error:", err)
		return
	}

	// buffer used during loop below
	audiobuf := make([]int16, frameSize*channels)

	dgv.Speaking(true)
	defer dgv.Speaking(false)

	for {
		// read data from ffmpeg stdout
		err = binary.Read(stdout, binary.LittleEndian, &audiobuf)
		if err == io.EOF || err == io.ErrUnexpectedEOF {
			break
		}
		if err != nil {
			fmt.Println("error reading from ffmpeg stdout :", err)
			break
		}
		if stop == true {
			run.Process.Kill()
			break
		}
		pcmChannel <- audiobuf
	}

	trackPlaying = false
}

// SendPCM idk, just copied this crap from github
func SendPCM(v *discordgo.VoiceConnection, pcm <-chan []int16) {
	mu.Lock()
	if sendpcm || pcm == nil {
		mu.Unlock()
		return
	}
	sendpcm = true
	mu.Unlock()
	defer func() { sendpcm = false }()

	opusEncoder, err := gopus.NewEncoder(frameRate, channels, gopus.Audio)
	if err != nil {
		fmt.Println("NewEncoder Error:", err)
		return
	}

	for {
		// read pcm from chan, exit if channel is closed.
		recv, ok := <-pcm
		if !ok {
			fmt.Println("PCM Channel closed.")
			return
		}

		// try encoding pcm frame with Opus
		opus, err := opusEncoder.Encode(recv, frameSize, maxBytes)
		if err != nil {
			fmt.Println("Encoding Error:", err)
			return
		}

		if v.Ready == false || v.OpusSend == nil {
			// fmt.Printf("Discordgo not ready for opus packets. %+v : %+v", v.Ready, v.OpusSend)
			return
		}
		// send encoded opus data to the sendOpus channel
		v.OpusSend <- opus
	}
}
