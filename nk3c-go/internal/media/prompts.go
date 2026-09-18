// prompts.go 运行时合成提示音（8kHz/16bit/单声道 PCM WAV）
// 不依赖外部音频资产：按节点类型生成差异化提示音（本项目语义：IVR 走线提示，非 TTS）
package media

import (
	"bytes"
	"encoding/binary"
	"math"
)

const sampleRate = 8000

// WavTone 生成一段 durMs 毫秒、由 freqs 多频率叠加的短提示音 WAV 字节
func WavTone(durMs int, freqs ...float64) []byte {
	n := sampleRate * durMs / 1000
	pcm := make([]byte, 44+n*2)
	// RIFF 头
	copy(pcm[0:4], "RIFF")
	binary.LittleEndian.PutUint32(pcm[4:8], uint32(36+n*2))
	copy(pcm[8:12], "WAVE")
	copy(pcm[12:16], "fmt ")
	binary.LittleEndian.PutUint32(pcm[16:20], 16)          // fmt 块长
	binary.LittleEndian.PutUint16(pcm[20:22], 1)           // PCM
	binary.LittleEndian.PutUint16(pcm[22:24], 1)           // 单声道
	binary.LittleEndian.PutUint32(pcm[24:28], sampleRate)  // 采样率
	binary.LittleEndian.PutUint32(pcm[28:32], sampleRate*2)// 字节率
	binary.LittleEndian.PutUint16(pcm[32:34], 2)           // 块对齐
	binary.LittleEndian.PutUint16(pcm[34:36], 16)          // 位深
	copy(pcm[36:40], "data")
	binary.LittleEndian.PutUint32(pcm[40:44], uint32(n*2))
	// 波形：多频叠加 + 5ms 淡入淡出（防爆音）
	for i := 0; i < n; i++ {
		t := float64(i) / sampleRate
		v := 0.0
		for _, f := range freqs {
			v += math.Sin(2 * math.Pi * f * t)
		}
		v = v / float64(len(freqs)) * 0.5
		
		switch {
		case i < sampleRate*5/1000:
			v *= float64(i) / float64(sampleRate*5/1000)
		case i > n-sampleRate*5/1000:
			v *= float64(n-i) / float64(sampleRate*5/1000)
		}
		binary.LittleEndian.PutUint16(pcm[44+i*2:], uint16(int16(v*32767)))
	}
	return pcm
}

// silence 静音段
func silence(durMs int) []byte {
	n := sampleRate * durMs / 1000
	return bytes.Repeat([]byte{0x00, 0x00}, n)
}

func concat(bs ...[]byte) []byte {
	var out bytes.Buffer
	for _, b := range bs {
		out.Write(b)
	}
	return out.Bytes()
}

// PromptFor 节点类型 → 提示音（差异化节拍，E2E 可凭时长分辨节点类型）
func PromptFor(nodeType string) []byte {
	switch nodeType {
	case "play": // 欢迎音：880Hz 300ms
		return WavTone(300, 880)
	case "menu": // 菜单：高低双音 800/1200Hz 各 150ms
		return concat(WavTone(150, 800), WavTone(150, 1200))
	case "question": // 问题：三连短音 1000Hz
		return concat(WavTone(90, 1000), silence(60), WavTone(90, 1000), silence(60), WavTone(90, 1000))
	case "voicemail": // 留言：长哔 600Hz 500ms
		return WavTone(500, 600)
	case "transfer": // 转接：回铃 425Hz 400ms
		return WavTone(400, 425)
	case "end": // 结束：下行双音 600→400
		return concat(WavTone(150, 600), WavTone(200, 400))
	}
	return WavTone(150, 800)
}
