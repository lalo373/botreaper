// Package brand holds the BotReaper product identity: display name and
// ASCII banner.
package brand

import "io"

// Name is the product display name.
const Name = "BotReaper"

// Art is the BotReaper ASCII banner.
const Art = `▄▄▄▄    ▒█████  ▄▄▄█████▓ ██▀███  ▓█████ ▄▄▄       ██▓███  ▓█████  ██▀███  
▓█████▄ ▒██▒  ██▒▓  ██▒ ▓▒▓██ ▒ ██▒▓█   ▀▒████▄    ▓██░  ██▒▓█   ▀ ▓██ ▒ ██▒
▒██▒ ▄██▒██░  ██▒▒ ▓██░ ▒░▓██ ░▄█ ▒▒███  ▒██  ▀█▄  ▓██░ ██▓▒▒███   ▓██ ░▄█ ▒
▒██░█▀  ▒██   ██░░ ▓██▓ ░ ▒██▀▀█▄  ▒▓█  ▄░██▄▄▄▄██ ▒██▄█▓▒ ▒▒▓█  ▄ ▒██▀▀█▄  
░▓█  ▀█▓░ ████▓▒░  ▒██▒ ░ ░██▓ ▒██▒░▒████▒▓█   ▓██▒▒██▒ ░  ░░▒████▒░██▓ ▒██▒
░▒▓███▀▒░ ▒░▒░▒░   ▒ ░░   ░ ▒▓ ░▒▓░░░ ▒░ ░▒▒   ▓▒█░▒▓▒░ ░  ░░░ ▒░ ░░ ▒▓ ░▒▓░
▒░▒   ░   ░ ▒ ▒░     ░      ░▒ ░ ▒░ ░ ░  ░ ▒   ▒▒ ░░▒ ░      ░ ░  ░  ░▒ ░ ▒░
 ░    ░ ░ ░ ░ ▒    ░        ░░   ░    ░    ░   ▒   ░░          ░     ░░   ░ 
 ░          ░ ░              ░        ░  ░     ░  ░            ░  ░   ░     
      ░ `

// Print writes the banner to w.
func Print(w io.Writer) {
	_, _ = io.WriteString(w, Art+"\n")
}
