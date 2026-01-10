package main

import (
	"log"
	"net/http"
	"os"
	"os/exec"
	"syscall"
	"unsafe"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

func main() {
	http.HandleFunc("/", serveIndex)
	http.HandleFunc("/ws", handleWebSocket)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting server on :%s", port)
	log.Printf("Open http://<your-ip>:%s on your mobile", port)
	log.Fatal(http.ListenAndServe(":"+port, nil))
}

func serveIndex(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, "index.html")
}

func handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println("Upgrade error:", err)
		return
	}
	defer conn.Close()

	// Start shell with PTY
	cmd := exec.Command("/bin/bash")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")

	ptmx, err := pty.Start(cmd)
	if err != nil {
		log.Println("PTY error:", err)
		return
	}
	defer ptmx.Close()

	// Set initial size
	setWinsize(ptmx, 80, 24)

	// PTY -> WebSocket
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); err != nil {
				return
			}
		}
	}()

	// WebSocket -> PTY
	for {
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}

		// Check for resize message: "resize:COLSxROWS"
		if len(msg) > 7 && string(msg[:7]) == "resize:" {
			var cols, rows uint16
			if _, err := parseSize(string(msg[7:]), &cols, &rows); err == nil {
				setWinsize(ptmx, cols, rows)
			}
			continue
		}

		if _, err := ptmx.Write(msg); err != nil {
			return
		}
	}
}

func parseSize(s string, cols, rows *uint16) (bool, error) {
	var c, r int
	_, err := parseXY(s, &c, &r)
	if err != nil {
		return false, err
	}
	*cols = uint16(c)
	*rows = uint16(r)
	return true, nil
}

func parseXY(s string, x, y *int) (bool, error) {
	n, err := scanXY(s, x, y)
	return n == 2, err
}

func scanXY(s string, x, y *int) (int, error) {
	var cx, cy int
	for i, c := range s {
		if c == 'x' {
			cx = i
		}
	}
	if cx == 0 {
		return 0, nil
	}
	// Parse before 'x'
	for i := 0; i < cx; i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, nil
		}
		*x = *x*10 + int(s[i]-'0')
	}
	// Parse after 'x'
	for i := cx + 1; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, nil
		}
		*y = *y*10 + int(s[i]-'0')
		cy = 1
	}
	if cy == 0 {
		return 1, nil
	}
	return 2, nil
}

func setWinsize(f *os.File, cols, rows uint16) {
	ws := struct {
		Row    uint16
		Col    uint16
		Xpixel uint16
		Ypixel uint16
	}{rows, cols, 0, 0}
	syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), syscall.TIOCSWINSZ, uintptr(unsafe.Pointer(&ws)))
}
