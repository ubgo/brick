package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"time"

	liveclient "github.com/ubgo/brick/examples/live-client"
)

// eventWait bounds every wait for a pushed event, so a broken flow fails
// instead of hanging.
const eventWait = 5 * time.Second

// ordersRE and roomRE find each component's markup in a page or answer.
var (
	ordersRE = regexp.MustCompile(`(?s)<ul[^>]*class="orders".*?</ul>`)
	roomRE   = regexp.MustCompile(`(?s)<div[^>]*class="room".*?</div>`)
)

// person is one browser: its cookies, and the transcript lines it prints
// under its own name.
type person struct {
	name string
	c    *liveclient.Client
	tr   liveclient.Transcript
}

// demo is the flow the guide shows: two signed-in viewers and one
// signed-out one, over a real HTTP server with real event streams.
func demo(w io.Writer, h http.Handler) error {
	srv := httptest.NewServer(h)
	defer srv.Close()
	people := map[string]*person{}
	for _, name := range []string{"Ada", "Bob", ""} {
		c := liveclient.New(srv.URL)
		if name != "" {
			if err := c.SetCookie(userCookie, name); err != nil {
				return err
			}
		}
		people[name] = &person{name: name, c: c, tr: liveclient.Transcript{W: w, C: c}}
	}
	ada, bob, anon := people["Ada"], people["Bob"], people[""]

	// 1. Ada opens the order list and its push stream.
	page, err := ada.c.Get("/orders/")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Ada: GET /orders/\n  %s\n", liveclient.Tidy(ordersRE.FindString(page)))
	adaOrders, err := liveclient.Find(page, "Orders")
	if err != nil {
		return err
	}
	stream, err := ada.c.Listen(page)
	if err != nil {
		return err
	}
	defer stream.Close()
	fmt.Fprintln(w, "Ada: GET /_brick/events?l=<listen token>  (open)")

	// 2. Bob ships order 1 from his own page. publish is never sent to
	// the client: the answer has no effects.
	bobPage, err := bob.c.Get("/orders/")
	if err != nil {
		return err
	}
	bobOrders, err := liveclient.Find(bobPage, "Orders")
	if err != nil {
		return err
	}
	fmt.Fprint(w, "Bob: ")
	ans, err := bob.tr.Act(bobOrders, "ship", 1)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  effects: %d\n", len(ans.Effects))
	if err := refreshed(w, ada, stream, adaOrders, ordersRE); err != nil {
		return err
	}

	// 3. A carrier's webhook ships order 2 from plain Go code.
	status, err := bob.c.Post("/webhook/shipped/2")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "webhook: POST /webhook/shipped/2 → %d\n", status)
	if err := refreshed(w, ada, stream, adaOrders, ordersRE); err != nil {
		return err
	}

	// 4. A signed-out viewer gets the page, but AuthorizeTopic keeps the
	// stream closed to them.
	anonPage, err := anon.c.Get("/orders/")
	if err != nil {
		return err
	}
	_, err = anon.c.Listen(anonPage)
	fmt.Fprintf(w, "signed out: GET /_brick/events → %v\n", err)

	// 5. Presence: who is in the lobby.
	adaRoomPage, err := ada.c.Get("/room/")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Ada: GET /room/\n  %s\n", liveclient.Tidy(roomRE.FindString(adaRoomPage)))
	adaRoom, err := liveclient.Find(adaRoomPage, "Room")
	if err != nil {
		return err
	}
	adaRoomStream, err := ada.c.Listen(adaRoomPage)
	if err != nil {
		return err
	}
	defer adaRoomStream.Close()
	if err := refreshed(w, ada, adaRoomStream, adaRoom, roomRE); err != nil {
		return err
	}
	bobRoomPage, err := bob.c.Get("/room/")
	if err != nil {
		return err
	}
	bobRoomStream, err := bob.c.Listen(bobRoomPage)
	if err != nil {
		return err
	}
	fmt.Fprintln(w, "Bob: GET /room/ and its stream")
	if err := refreshed(w, ada, adaRoomStream, adaRoom, roomRE); err != nil {
		return err
	}
	if err := bobRoomStream.Close(); err != nil {
		return err
	}
	fmt.Fprintln(w, "Bob closes the tab")
	return refreshed(w, ada, adaRoomStream, adaRoom, roomRE)
}

// refreshed waits for p's stream to deliver a refresh, then does what
// the runtime does with it: a quiet $refresh of the listening component.
func refreshed(w io.Writer, p *person, s *liveclient.Stream, root *liveclient.Root, show *regexp.Regexp) error {
	topic, err := s.Next(eventWait)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "%s: ← event: refresh  data: %s\n%s: ", p.name, topic, p.name)
	ans, err := p.tr.Act(root, "$refresh")
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "  %s\n", liveclient.Tidy(show.FindString(ans.HTML)))
	return nil
}
