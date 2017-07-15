package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"

	"regexp"

	"encoding/json"
	"os/user"

	"io/ioutil"
	"path/filepath"

	"strconv"

	"os/signal"

	"golang.org/x/crypto/ssh/terminal"
)

func printUsage() {
	fmt.Println("Usage: hless source_file")
	fmt.Println("  -e edit config")
}

type Config struct {
	Foreground map[string]string
	Background map[string]string
	Aliases    map[string]string
}

type formatter struct {
	regex   *regexp.Regexp
	colors  map[string]string
	aliases map[string]string
}

var hexColor = regexp.MustCompile("^#([[:xdigit:]]{2})([[:xdigit:]]{2})([[:xdigit:]]{2})$")

func trueColorSequence(hexcolor string) (string, error) {

	m := hexColor.FindStringSubmatch(hexcolor)

	if len(m) != 4 {
		return "", fmt.Errorf("invalid color: '%s'", hexcolor)
	}

	r, _ := strconv.ParseInt(m[1], 16, 32)
	g, _ := strconv.ParseInt(m[2], 16, 32)
	b, _ := strconv.ParseInt(m[3], 16, 32)

	return fmt.Sprintf("\x1b[38;2;%d;%d;%dm", r, g, b), nil
}

func editConfig() {
	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	err = os.MkdirAll(filepath.FromSlash(usr.HomeDir+"/.config/hless"), 0700)
	if err != nil {
		log.Fatal(err)
	}

	editor := os.Getenv("EDITOR")

	if len(editor) == 0 {
		editor = "vim"
	}

	cmd := exec.Command(editor, filepath.FromSlash(usr.HomeDir+"/.config/hless/default.json"))
	cmd.Stdout = os.Stdout
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	err = cmd.Run()
	if err != nil {
		log.Fatal(err)
	}
}

func initFormatter() *formatter {
	c := &Config{
	/*HighlightWords: make(map[string]string),
	Aliases:        make(map[string]string),*/
	}

	usr, err := user.Current()
	if err != nil {
		log.Fatal(err)
	}

	data, err := ioutil.ReadFile(filepath.FromSlash(usr.HomeDir + "/.config/hless/default.json"))
	if err != nil {
		log.Println(err)
		return &formatter{}
	}

	err = json.Unmarshal(data, c)
	if err != nil {
		log.Fatal(err)
	}

	f := &formatter{
		aliases: c.Aliases,
		colors:  make(map[string]string),
	}

	for key, fore := range c.Foreground {
		fs, err := trueColorSequence(fore)
		if err != nil {
			log.Fatal(err)
		}

		f.colors[key] = fs
	}

	for key, fore := range c.Background {
		bs, err := trueColorSequence(fore)
		if err != nil {
			log.Fatal(err)
		}
		if old, ok := f.colors[key]; ok {
			f.colors[key] = old + bs
		} else {
			f.colors[key] = bs
		}
	}

	tmp := &bytes.Buffer{}

	_, _ = io.WriteString(tmp, "(")

	numKeywords := len(f.colors) + len(f.aliases)

	i := 0
	for key := range f.aliases {
		i++
		_, _ = io.WriteString(tmp, key)
		if i < numKeywords {
			_, _ = io.WriteString(tmp, "|")
		}
	}

	for key := range f.colors {
		i++
		_, _ = io.WriteString(tmp, key)
		if i < numKeywords {
			_, _ = io.WriteString(tmp, "|")
		}
	}
	_, _ = io.WriteString(tmp, ")")

	f.regex = regexp.MustCompile(tmp.String())

	return f
}

func (f *formatter) format(line string) string {
	if f.regex == nil {
		return line
	}
	return f.regex.ReplaceAllStringFunc(line, func(s string) string {
		if substitute, ok := f.aliases[s]; ok {
			s = substitute
		}
		if color, ok := f.colors[s]; ok {
			s = color + s + "\x1b[0m"
		}
		return s
	})
}

func main() {

	if terminal.IsTerminal(int(os.Stdin.Fd())) && len(os.Args) == 1 {
		printUsage()
		return
	}

	var source io.ReadCloser

	if len(os.Args) > 1 {

		if os.Args[1] == "-e" {
			editConfig()
			return
		}
		var err error
		source, err = os.Open(os.Args[1])
		if err != nil {
			log.Fatal(err)
		}
	} else {
		source = os.Stdin
	}

	f := initFormatter()

	cmd := exec.Command("less", "-n", "-R", "-")
	cmd.Stdout = os.Stdout

	dest, err := cmd.StdinPipe()
	if err != nil {
		log.Fatal(err)
	}

	stop := make(chan struct{})

	signal.Ignore(os.Interrupt)

	go func() {
		defer dest.Close() // nolint: errcheck
		dst := bufio.NewWriter(dest)
		dst.Flush() // nolint: errcheck
		src := bufio.NewScanner(source)
		for src.Scan() {
			line := src.Text()
			line = f.format(line)

			_, err = dst.WriteString(line)
			if err != nil {
				return
			}
			_, _ = dst.WriteString("\n")
		}
	}()

	err = cmd.Run()
	close(stop)
	if err != nil {
		log.Fatal(err)
	}
}
