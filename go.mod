module github.com/ZaparooProject/zaparoo-core

go 1.22.0

toolchain go1.23.4

<<<<<<< HEAD
=======
replace github.com/gdamore/tcell/v2 v2.8.0 => github.com/asturur/tcell/v2 v2.8.2

require github.com/rthornton128/goncurses v0.0.0-20220628231859-fd57939296e5

>>>>>>> main
require golang.org/x/exp v0.0.0-20241217172543-b2144cdd0a67

require gopkg.in/ini.v1 v1.67.0

require github.com/fsnotify/fsnotify v1.6.0

require (
	github.com/bendahl/uinput v1.7.0
	github.com/libp2p/zeroconf/v2 v2.2.0 // indirect
	github.com/miekg/dns v1.1.55 // indirect
	github.com/txn2/txeh v1.4.0 // indirect
	golang.org/x/mod v0.22.0 // indirect
	golang.org/x/net v0.32.0 // indirect
	golang.org/x/sys v0.29.0 // indirect
	golang.org/x/tools v0.28.0 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.0.0
)

require (
	github.com/creack/goselect v0.1.2 // indirect
<<<<<<< HEAD
	github.com/gdamore/encoding v1.0.0 // indirect
	github.com/gdamore/tcell/v2 v2.7.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.15 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/term v0.27.0 // indirect
=======
	github.com/gdamore/encoding v1.0.1 // indirect
	github.com/lucasb-eyer/go-colorful v1.2.0 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/mattn/go-runewidth v0.0.16 // indirect
	github.com/rivo/uniseg v0.4.7 // indirect
	golang.org/x/term v0.28.0 // indirect
>>>>>>> main
)

require (
	github.com/adrg/xdg v0.5.3
	github.com/andygrunwald/vdf v1.1.0
	github.com/clausecker/nfc/v2 v2.1.4
	github.com/ebfe/scard v0.0.0-20230420082256-7db3f9b7c8a7
	github.com/gdamore/tcell/v2 v2.8.0
	github.com/go-chi/chi/v5 v5.0.12
	github.com/go-chi/cors v1.2.1
	github.com/gobwas/glob v0.2.3
	github.com/gocarina/gocsv v0.0.0-20230616125104-99d496ca653d
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.3
	github.com/hsanjuan/go-ndef v0.0.1
	github.com/olahol/melody v1.2.1
	github.com/pelletier/go-toml/v2 v2.2.3
	github.com/rivo/tview v0.0.0-20241227133733-17b7edb88c57
	github.com/rs/zerolog v1.31.0
	github.com/wizzomafizzo/mrext v0.1.2
	go.bug.st/serial v1.6.2
	go.etcd.io/bbolt v1.3.9
	golang.org/x/sync v0.10.0
	golang.org/x/text v0.21.0
)
