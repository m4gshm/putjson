package main

import (
	"flag"
	"fmt"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"path"
	"path/filepath"
	"strings"
)

const (
	name   = "putjson"
	indent = "  "
)

var (
	input     = flag.String("input", "", "input directory; must be set")
	output    = flag.String("output", "", "output directory; must be set")
	extension = flag.String("extensions", "", "comma delimited input filed extensions")
	_start    = flag.String("startToken", "{{", "start block symbols")
	_end      = flag.String("endToken", "}}", "end block symbols")
)

type token string

const (
	start token = "start"
	end   token = "end"
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage of "+name+":\n")
	_, _ = fmt.Fprintf(os.Stderr, "\t"+name+" [flags]\n")
	_, _ = fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	log.SetPrefix(name + ": ")

	flag.Usage = usage
	flag.Parse()

	var (
		startVal = *_start
		endVal   = *_end
	)
	if startVal == endVal {
		log.Fatalf("start block %v must be different with end one %v", start, end)
	}

	extensions := map[string]bool{}
	if extension != nil {
		split := strings.Split(*extension, ",")
		for _, ext := range split {
			if len(ext) > 0 {
				extensions["."+ext] = true
			}
		}
	}
	isIncluded := func(path string) bool {
		return true
	}

	if len(extensions) > 0 {
		isIncluded = func(file string) bool {
			return extensions[path.Ext(file)]
		}
	}

	type info struct {
		isStart, isEnd bool
	}

	tokens := map[string]info{
		startVal: {isStart: true},
		endVal:   {isEnd: true},
	}

	rootInput := *input
	if len(rootInput) == 0 {
		log.Println("input dir not defined")
		flag.Usage()
		os.Exit(1)
	}
	rootOutput := *output
	if len(rootOutput) == 0 {
		log.Println("output dir not defined")
		flag.Usage()
		os.Exit(1)
	}

	if err := os.MkdirAll(rootInput, os.ModePerm); err != nil {
		log.Fatalf("error of create input dir %v: %v", rootInput, err)
	}

	if err := filepath.Walk(rootInput, func(filePath string, fileInfo fs.FileInfo, err error) error {
		if err != nil {
			return err
		} else if fileInfo.IsDir() {
			return nil
		} else if !isIncluded(filePath) {
			log.Println("exclude by extension", filePath)
			return nil
		}

		raw, err := ioutil.ReadFile(path.Clean(filePath))
		if err != nil {
			return fmt.Errorf("file %v: %w", filePath, err)
		}

		blocks := make([]string, 0)

		content := string(raw)
		const noStart = -1
		startBlockPos := noStart
		position := 0
		for position < len(content) {
			visited := 0
			for value, i := range tokens {
				nextPosition := position + len(value)
				if nextPosition > len(content) {
					nextPosition = len(content)
				}
				part := content[position:nextPosition]
				if part == value {
					if i.isStart {
						if startBlockPos != noStart {
							log.Fatalf("detected start block but previos start is not closed, position %d in '%v'",
								position, getPart(content, position))
						}
						startBlockPos = nextPosition
					} else if i.isEnd {
						if startBlockPos == noStart {
							log.Fatalf("detected end block but without predefined start, position %d in '%v'",
								position, getPart(content, position))
						}
						blocks = append(blocks, content[startBlockPos:position])
						startBlockPos = noStart
					} else {
						log.Fatalf("unexpected token %v at %d", value, position)
					}
					visited = len(value)
					break
				} else {
					visited = 1
				}
			}
			position += visited
		}

		if len(blocks) > 0 {
			var outFileName string
			outFileName, err = filepath.Rel(rootInput, filePath)
			if err != nil {
				log.Fatalf("relative path comute error, root %v, target %v : %v", rootInput, filePath, err)
			}

			if err = os.MkdirAll(rootOutput, os.ModePerm); err != nil {
				log.Fatalf("error of create output dir %v: %v", rootOutput, err)
			}
			ext := filepath.Ext(outFileName)
			if len(ext) > 0 {
				outFileName = outFileName[:len(outFileName)-len(ext)] + ".json"
			}
			outFilePath := filepath.Join(rootOutput, outFileName)
			var outFile *os.File
			outFile, err = os.Create(outFilePath)
			if err != nil {
				log.Fatalf("error of create output file %v: %v", outFilePath, err)
			}
			write(outFile, "{\n")

			for i, b := range blocks {
				if i > 0 {
					write(outFile, ",\n")
				}
				write(outFile, fmt.Sprintf("%v\"block_%v\":\"%v\"", indent, i, b))
			}
			write(outFile, "\n}\n")
			_ = outFile.Sync()
			_ = outFile.Close()
		}

		return nil
	}); err != nil {
		log.Fatalf("walkDir %v: %v", rootInput, err)
	}
}

func write(file *os.File, content string) {
	if _, err := file.WriteString(content); err != nil {
		log.Fatalf("error of write file %v: %v", file.Name(), err)
	}
}

func getPart(content string, position int) string {
	from := position - 10
	to := position + 10
	if from < 0 {
		to -= from
		from = 0
	}
	if to > len(content) {
		to = len(content)
	}
	part := content[from:to]
	return part
}
