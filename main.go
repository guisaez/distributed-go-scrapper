package main

import (
	"bytes"
	"fmt"
	"io"
	"log"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/anthdm/hollywood/actor"
	"golang.org/x/net/html"
)

type VisitFunc func(io.Reader) error 

type VisitRequest struct {
	links []string
	visitFunc VisitFunc
}

func NewVisitRequest(links []string) VisitRequest {
	return VisitRequest{
		links: links,
		visitFunc: func(r io.Reader) error { // Define any additional logic to the function here. 
			fmt.Println("========================")
			b, err := io.ReadAll(r)
			if err != nil {
				return err
			}
			fmt.Println(string(b))
			fmt.Println("========================")
			return nil
		},
	}
}

type Visitor struct { // Worker
	managerPID *actor.PID
	URL *url.URL
	visitFunc VisitFunc
}

func NewVisitor(url*url.URL, mpid *actor.PID, visitFunc  VisitFunc) actor.Producer {

	return func() actor.Receiver{
		return &Visitor{
			URL: url,
			managerPID: mpid,
			visitFunc: visitFunc,
		}
	}
}

func (v *Visitor) Receive(c *actor.Context) {
	switch c.Message().(type) {
	case actor.Started:
		slog.Info("visitor started", "url", v.URL)
		links, err := v.doVisit(v.URL.String(), v.visitFunc)
		if err != nil {
			slog.Error("visit error", "err", err)
			return 
		}
		c.Send(v.managerPID, NewVisitRequest(links))
		c.Engine().Poison(c.PID())
	case actor.Stopped:
		slog.Info("visitor stopped", "url", v.URL)
	}
}

func (v *Visitor) extractLinks(body io.Reader) ([]string, error) {
	links := make([]string, 0)
	tokenizer := html.NewTokenizer(body)

	for {
		tokenType := tokenizer.Next()
		if tokenType == html.ErrorToken {
			return links, nil
		}

		if tokenType == html.StartTagToken {
			token := tokenizer.Token()
			if token.Data == "a" {
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						lurl, err := url.Parse(attr.Val)
						if err != nil {
							return links, err
						}
						actuallink := v.URL.ResolveReference(lurl)
						links = append(links, actuallink.String())
					}
				}
			}
		}
	}
}

func(v *Visitor) doVisit(link string, visit VisitFunc) ([]string, error) {

	baseURL, err := url.Parse(link)
	if err != nil {
		return []string{}, err
	}

	resp, err := http.Get(baseURL.String())
	if err != nil {
		return []string{}, err
	}

	w := &bytes.Buffer{}
	r := io.TeeReader(resp.Body, w) // returs a Reader that writes into w
	
	if err := visit(r); err != nil {
		return []string{}, err
	}

	links, err := v.extractLinks(w)
	if err != nil {
		return []string{}, err
	}

	return links, nil
}



type Manager struct {
	visitors map[*actor.PID]bool
	visited map[string]bool
}

func NewManager() actor.Producer {
	return func() actor.Receiver{
		return &Manager{
			visitors: make(map[*actor.PID]bool),
			visited: make(map[string]bool),
		}
	}
}

func (m *Manager) Receive(c *actor.Context) {
	switch msg := c.Message().(type) {
	case VisitRequest:
		m.handleVisitRequest(c, msg)
	case actor.Started:
		slog.Info("manager started")
	case actor.Stopped:
		slog.Info("manager stopped")
	}
}

func (m *Manager) handleVisitRequest(c *actor.Context, msg VisitRequest) error {
	for _, link := range msg.links {
		if _, ok := m.visited[link]; !ok {
			slog.Info("visiting url", "url", link)
			baseURL, err := url.Parse(link)
			if err != nil {
				return err
			}
			c.SpawnChild(NewVisitor(baseURL, c.PID(), msg.visitFunc ), "visitor/" + link)
			m.visited[link] = true
		}
	}
	
	return nil
}



func main() {
	e, err := actor.NewEngine(actor.NewEngineConfig())
	if err != nil {
		log.Fatal(err)
	}

	pid := e.Spawn(NewManager(), "manager")

	time.Sleep(time.Millisecond * 200)
	
	e.Send(pid,NewVisitRequest([]string{"https://levenue.com"}))
	e.Send(pid,NewVisitRequest([]string{"https://fulltimegodev.com"}))
	
	time.Sleep(time.Second * 1000)

}



