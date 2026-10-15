package main

import (
	"context"
	"flag"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"image"
	"log"
	"os"
	"path/filepath"
	"s7-ai-studio/internal/service"
	"s7-ai-studio/internal/ui"
	"s7-ai-studio/platform"
	"time"
)

var version = "0.5.0"

func main() {
	smokeDir := flag.String("smoke-test", "", "验证自有回环演示并保存证据后退出")
	flag.Parse()
	platform.Setup()
	platform.PrewarmFonts()
	a := app.NewWithID("com.s7aistudio.desktop")
	platform.ConfigureApp(a)
	opts := service.Options{}
	if *smokeDir != "" {
		opts.RecordDir = filepath.Join(*smokeDir, "records")
	}
	c := service.New(opts)
	d := ui.NewDesktop(a, c, version)
	d.Show()
	result := make(chan error, 1)
	if *smokeDir != "" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			e := runSmoke(ctx, c, func() image.Image {
				time.Sleep(500 * time.Millisecond)
				var im image.Image
				fyne.DoAndWait(func() { im = d.Window().Canvas().Capture() })
				return im
			}, *smokeDir)
			result <- e
			fyne.Do(a.Quit)
		}()
	}
	a.Run()
	closeErr := d.Shutdown()
	if closeErr != nil {
		log.Printf("保存记录失败: %v", closeErr)
	}
	if *smokeDir != "" {
		select {
		case e := <-result:
			if e != nil {
				log.Printf("GUI 验证失败: %v", e)
				os.Exit(1)
			}
		default:
			log.Print("GUI 验证被中断")
			os.Exit(1)
		}
	}
	if closeErr != nil {
		os.Exit(1)
	}
}
