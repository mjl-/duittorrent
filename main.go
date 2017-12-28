/*
DuitTorrent is a simple bittorrent client developed as demo for duit, the Developer UI Toolkit.
*/
package main

import (
	"flag"
	"fmt"
	"image"
	"log"
	"os"
	"sort"
	"strconv"
	"time"

	"9fans.net/go/draw"
	"mjl/duit"

	"github.com/anacrolix/dht"
	"github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"
)

const (
	ColStatus = iota
	ColName
	ColHave
	ColTotal
	ColETA
	ColDownrate
	ColUprate
	NCol
)

var (
	client  *torrent.Client
	config  *torrent.Config
	gotInfo chan *torrent.Torrent

	list                 *duit.Gridlist
	toggleActive, remove *duit.Button
	details              *duit.Box
	bold                 *draw.Font

	columnNames = []string{
		"status",
		"name",
		"completed",
		"total",
		"eta",
		"downrate",
		"uprate",
	}
	columnHalign = []duit.Halign{
		duit.HalignLeft,
		duit.HalignLeft,
		duit.HalignRight,
		duit.HalignRight,
		duit.HalignRight,
		duit.HalignRight,
		duit.HalignRight,
	}

	torrentWant  map[metainfo.Hash]bool              // whether we currently want to download this torrent
	torrentStats map[metainfo.Hash]torrent.ConnStats // previous stats, for calculating rate & eta

	TickInterval = 2 * time.Second
)

func check(err error, msg string) {
	if err != nil {
		log.Fatalf("%s: %s\n", msg, err)
	}
}

func updateRow(row *duit.Gridrow, updateStats bool) {
	const (
		ColStatus = iota
		ColName
		ColHave
		ColTotal
		ColETA
		ColDownrate
		ColUprate
		NCol
	)
	t := row.Value.(*torrent.Torrent)

	row.Values[ColName] = t.String()
	i := t.Info()
	var status string
	if i == nil {
		status = "starting"
	} else if t.Seeding() {
		status = "seeding"
	} else if !torrentWant[t.InfoHash()] {
		status = "paused"
	} else if t.BytesMissing() == 0 {
		status = "finished"
	} else {
		status = "downloading"
	}
	row.Values[ColStatus] = status

	have := "0"
	total := "?"
	if i != nil {
		have = formatSize(t.BytesCompleted())
		total = formatSize(t.BytesMissing() + t.BytesCompleted())
	}
	row.Values[ColHave] = have
	row.Values[ColTotal] = total

	if !updateStats {
		return
	}
	nstats := t.Stats().ConnStats
	ostats, ok := torrentStats[t.InfoHash()]
	torrentStats[t.InfoHash()] = nstats
	if !ok {
		return
	}

	downrate := (nstats.DataBytesRead - ostats.DataBytesRead) * int64(time.Second) / int64(TickInterval)
	uprate := (nstats.BytesWritten - ostats.DataBytesWritten) * int64(time.Second) / int64(TickInterval)
	row.Values[ColDownrate] = fmt.Sprintf("%dk", downrate/1024)
	row.Values[ColUprate] = fmt.Sprintf("%dk", uprate/1024)

	done := nstats.DataBytesRead - ostats.DataBytesRead
	if done <= 0 {
		row.Values[ColETA] = "∞"
		return
	}
	secs := time.Duration(float64(TickInterval)*float64(t.BytesMissing())/float64(done)) / time.Second
	hours := secs / 3600
	mins := (secs % 3600) / 60
	secs = secs % 60
	if hours > 0 {
		row.Values[ColETA] = fmt.Sprintf("%dh%02dm", hours, mins)
	} else if mins > 0 {
		row.Values[ColETA] = fmt.Sprintf("%02dm%02ds", mins, secs)
	} else {
		row.Values[ColETA] = fmt.Sprintf("%02ds", secs)
	}
}

func formatSize(v int64) string {
	return fmt.Sprintf("%.1fm", float64(v)/(1024*1024))
}

func findRow(t *torrent.Torrent) *duit.Gridrow {
	for _, row := range list.Rows {
		if row.Value == t {
			return row
		}
	}
	return nil
}

func updateDetails(t *torrent.Torrent) {
	if t == nil {
		details.Kids = nil
		return
	}
	i := t.Info()
	if i == nil {
		details.Kids = duit.NewKids(&duit.Label{
			Text: "fetching metainfo...",
		})
		return
	}

	var uis []duit.UI

	_box := func(top int, ui duit.UI) *duit.Box {
		return &duit.Box{
			Padding: duit.Space{top, 0, 0, 0},
			Width:   -1,
			Kids:    duit.NewKids(ui),
		}
	}
	box := func(ui duit.UI) *duit.Box {
		return _box(4, ui)
	}
	titleBox := func(ui duit.UI) *duit.Box {
		return _box(10, ui)
	}

	makeGrid := func(elems ...string) *duit.Grid {
		kids := make([]duit.UI, len(elems))
		for i, s := range elems {
			kids[i] = &duit.Label{Text: s}
		}
		return &duit.Grid{
			Columns: 2,
			Padding: []duit.Space{
				{2, 4, 2, 0},
				{2, 0, 2, 4},
			},
			Width: -1,
			Kids:  duit.NewKids(kids...),
		}
	}

	var fileUIs []duit.UI
	for _, f := range t.Files() {
		name := &duit.Label{Text: f.Path()}
		size := &duit.Label{Text: formatSize(f.Length())}
		fileUIs = append(fileUIs, name, size)
	}
	filesGrid := &duit.Grid{
		Columns: 2,
		Padding: []duit.Space{
			{2, 4, 2, 0},
			{2, 0, 2, 4},
		},
		Width:  -1,
		Halign: []duit.Halign{duit.HalignLeft, duit.HalignRight},
		Kids:   duit.NewKids(fileUIs...),
	}
	uis = append(uis,
		box(&duit.Label{Text: "Files", Font: bold}),
		box(filesGrid),
	)

	uis = append(uis,
		titleBox(&duit.Label{Text: "Info", Font: bold}),
		box(makeGrid(
			"Pieces", fmt.Sprintf("%d", t.NumPieces()),
			"Piece length", fmt.Sprintf("%d", i.PieceLength),
			"Name", i.Name,
		)),
	)

	var announceUIs []duit.UI
	al := t.Metainfo().AnnounceList.DistinctValues()
	announces := make([]string, 0, len(al))
	for k := range al {
		announces = append(announces, k)
	}
	sort.Slice(announces, func(i, j int) bool {
		return announces[i] < announces[j]
	})
	for _, v := range announces {
		announceUIs = append(announceUIs, &duit.Box{
			Width: -1,
			Kids:  duit.NewKids(&duit.Label{Text: v}),
		})
	}
	uis = append(uis,
		titleBox(&duit.Label{Text: "Announces", Font: bold}),
		&duit.Box{
			ChildMargin: image.Pt(0, 4),
			Kids:        duit.NewKids(announceUIs...),
		},
	)

	ts := t.Stats()
	connGrid := makeGrid(
		"Active peers", fmt.Sprintf("%d", ts.ActivePeers),
		"Half open peers", fmt.Sprintf("%d", ts.HalfOpenPeers),
		"Pending peers", fmt.Sprintf("%d", ts.PendingPeers),
		"Total peers", fmt.Sprintf("%d", ts.TotalPeers),
		"Chunks written", fmt.Sprintf("%d", ts.ConnStats.ChunksWritten),
		"Chunks read", fmt.Sprintf("%d", ts.ConnStats.ChunksRead),
		"Data written", formatSize(ts.ConnStats.BytesWritten),
		"Data read", formatSize(ts.ConnStats.BytesRead),
		"Total written (including overhead)", formatSize(ts.ConnStats.DataBytesWritten),
		"Total read", formatSize(ts.ConnStats.DataBytesRead),
	)

	uis = append(uis,
		titleBox(&duit.Label{Text: "Connection stats", Font: bold}),
		box(connGrid),
	)

	details.Kids = duit.NewKids(uis...)
}

func updateButtons(t *torrent.Torrent) {
	toggleActive.Disabled = t == nil
	remove.Disabled = t == nil

	toggleActive.Text = "start"
	if t != nil && torrentWant[t.InfoHash()] {
		toggleActive.Text = "pause"
	}
}

func selected() *torrent.Torrent {
	l := list.Selected()
	if len(l) == 0 {
		return nil
	}

	i := l[0]
	return list.Rows[i].Value.(*torrent.Torrent)
}

func parseRate(s string) (rate.Limit, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, err
	}
	v *= 1024
	if v == 0 {
		return rate.Inf, nil
	}
	return rate.Limit(v), nil
}

func main() {
	log.SetFlags(0)
	flag.Usage = func() {
		log.Println("usage: duittorrent")
		flag.PrintDefaults()
	}
	flag.Parse()
	args := flag.Args()
	if len(args) != 0 {
		flag.Usage()
		os.Exit(2)
	}

	var err error
	config = &torrent.Config{
		DHTConfig: dht.ServerConfig{
			StartingNodes: dht.GlobalBootstrapAddrs,
		},
		UploadRateLimiter:   rate.NewLimiter(rate.Inf, 16*1024),
		DownloadRateLimiter: rate.NewLimiter(rate.Inf, 16*1024),
	}
	client, err = torrent.NewClient(config)
	check(err, "new torrent client")

	dui, err := duit.NewDUI("torrent", "850x600")
	check(err, "new dui")

	bold = dui.Display.DefaultFont
	if os.Getenv("boldfont") != "" {
		bold, err = dui.Display.OpenFont(os.Getenv("boldfont"))
		check(err, "open bold font")
	}

	gotInfo = make(chan *torrent.Torrent)
	torrentWant = map[metainfo.Hash]bool{}
	torrentStats = map[metainfo.Hash]torrent.ConnStats{}

	toggleActive = &duit.Button{
		Text: "", // pause or start
		Click: func(r *duit.Result) {
			t := selected()
			if t == nil {
				log.Println("should not happen: toggle while no torrent selected")
				return
			}

			r.Layout = true
			h := t.InfoHash()
			nv := !torrentWant[h]
			torrentWant[h] = nv
			updateButtons(t)
			updateDetails(t)
			i := t.Info()
			if i == nil {
				return
			}
			if nv {
				t.DownloadAll()
			} else {
				t.CancelPieces(0, t.NumPieces())
			}
		},
	}
	remove = &duit.Button{
		Text: "remove",
		Click: func(r *duit.Result) {
			l := list.Selected()
			if len(l) == 0 {
				log.Println("should not happen: remove of torrent while none selected")
				return
			}
			r.Layout = true
			i := l[0]
			row := list.Rows[i]
			t := row.Value.(*torrent.Torrent)
			t.Drop()
			list.Rows = append(list.Rows[:i], list.Rows[i+1:]...)
			updateButtons(nil)
			updateDetails(nil)
		},
	}
	var input *duit.Field
	input = &duit.Field{
		Placeholder: "magnet...",
		Keys: func(m draw.Mouse, k rune, r *duit.Result) {
			if k == '\n' && len(input.Text) > 0 {
				uri := input.Text
				input.Text = ""
				r.Consumed = true
				r.Redraw = true
				t, err := client.AddMagnet(uri)
				if err != nil {
					return
				}
				nrow := &duit.Gridrow{
					Values:   make([]string, NCol),
					Value:    t,
					Selected: true,
				}
				updateRow(nrow, false)
				for _, row := range list.Rows {
					row.Selected = false
				}
				list.Rows = append([]*duit.Gridrow{nrow}, list.Rows...)
				updateButtons(t)
				updateDetails(t)
				go func() {
					<-t.GotInfo()
					gotInfo <- t
				}()
			}
		},
	}
	var maxUp *duit.Field
	maxUp = &duit.Field{
		Text: "0",
		Keys: func(m draw.Mouse, k rune, r *duit.Result) {
			if k == '\n' && len(maxUp.Text) > 0 {
				s := maxUp.Text
				r.Consumed = true

				v, err := parseRate(s)
				if err != nil {
					r.Redraw = true
					log.Printf("bad rate: %s\n", err)
					maxUp.Text = ""
					return
				}
				config.UploadRateLimiter.SetLimit(v)
			}
		},
	}
	var maxDown *duit.Field
	maxDown = &duit.Field{
		Text: "0",
		Keys: func(m draw.Mouse, k rune, r *duit.Result) {
			if k == '\n' && len(maxDown.Text) > 0 {
				s := maxDown.Text
				r.Consumed = true

				v, err := parseRate(s)
				if err != nil {
					r.Redraw = true
					log.Printf("bad rate: %s\n", err)
					maxDown.Text = ""
					return
				}
				config.DownloadRateLimiter.SetLimit(v)
			}
		},
	}

	bar := &duit.Box{
		Padding:     duit.SpaceXY(6, 4),
		ChildMargin: image.Pt(6, 4),
		Kids: duit.NewKids(
			toggleActive,
			remove,
			&duit.Box{
				Width: 300,
				Kids:  duit.NewKids(input),
			},
			&duit.Label{Text: "max up kb/s:"},
			&duit.Box{
				Width: 80,
				Kids:  duit.NewKids(maxUp),
			},
			&duit.Label{Text: "max down kb/s:"},
			&duit.Box{
				Width: 80,
				Kids:  duit.NewKids(maxDown),
			},
		),
	}
	list = &duit.Gridlist{
		Halign:  columnHalign,
		Padding: duit.SpaceXY(2, 2),
		Striped: true,
		Header: duit.Gridrow{
			Values: columnNames,
		},
		Changed: func(index int, r *duit.Result) {
			row := list.Rows[index]
			var t *torrent.Torrent
			if row.Selected {
				t = row.Value.(*torrent.Torrent)
			}
			updateButtons(t)
			updateDetails(t)
			r.Layout = true
		},
	}
	listBox := &duit.Scroll{
		MaxHeight: -1,
		Child: &duit.Box{
			Padding: duit.SpaceXY(6, 4),
			Kids:    duit.NewKids(list),
		},
	}
	details = &duit.Box{
		Padding: duit.SpaceXY(6, 4),
	}
	detailsBox := &duit.Scroll{
		MaxHeight: -1,
		Child:     details,
	}
	vertical := &duit.Vertical{
		Split: func(height int) []int {
			return []int{height / 2, height - height/2}
		},
		Kids: duit.NewKids(
			listBox,
			detailsBox,
		),
	}
	dui.Top = &duit.Box{
		Kids: duit.NewKids(
			bar,
			vertical,
		),
	}

	updateButtons(nil)
	updateDetails(nil)
	dui.Render()

	tick := time.Tick(TickInterval)

	for {
		select {
		case e := <-dui.Events:
			dui.Event(e)

		case <-tick:
			for _, row := range list.Rows {
				updateRow(row, true)
			}
			t := selected()
			updateDetails(t)
			dui.Render()

		case t := <-gotInfo:
			// torrent could have been closed in the mean time
			row := findRow(t)
			if row == nil {
				continue
			}

			if torrentWant[t.InfoHash()] {
				t.DownloadAll()
			}
			updateRow(row, false)
			if row.Selected {
				updateButtons(t)
				updateDetails(t)
			}
			dui.Render()
		}
	}
}
