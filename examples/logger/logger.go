package main

import (
	"io"
	"log"

	pastebin "github.com/dutchcoders/gopastebin"
	"ilya.app/pbmon"
)

func main() {
	psb, err := pbmon.New()
	if err != nil {
		log.Fatal(err)
	}

	psb.OnNew = func(p pastebin.Paste, r io.ReadCloser) error {
		defer r.Close() // do not forget to close the body
		log.Printf("title=%s user=%s syntax=%s url=%s\n", p.Title, p.User, p.Syntax, p.FullURL)
		return nil
	}
	log.Fatal(psb.Do(pbmon.DefaultRecentSize, pbmon.DefaultTimeout))
}
