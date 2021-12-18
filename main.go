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
	"regexp"
	"strings"
)

const (
	name   = "putjson"
	indent = "  "
)

var (
	input       = flag.String("input", "", "input directory; must be set")
	output      = flag.String("output", "", "output directory; must be set")
	fileMatcher = flag.String("fileMatcher", `\d+_(?P<language>[A-Za-z]{2})_[A-Za-z]{2}_.+.txt`,
		"regular expression fo file name matching")
	startToken   = flag.String("startToken", "{{", "start block symbols")
	endToken     = flag.String("endToken", "}}", "end block symbols")
	outDirSuffix = flag.String("outSuffix", "-out", "output subdirectory suffix")
	verbose      = flag.Bool("v", false, "log verbose")
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
		start = *startToken
		end   = *endToken
	)
	if start == end {
		log.Fatalf("start block %v must be different with end one %v", start, end)
	}

	isIncluded := func(path string) bool {
		return true
	}

	if fileMatcher == nil || len(*fileMatcher) == 0 {
		log.Fatalf("fileMatcher regexp must be defined")
	}

	expr := *fileMatcher
	re, err := regexp.Compile(expr)
	if err != nil {
		log.Fatalf("invalid fileMatcher %v", expr)
	}

	suffix := ""
	if outDirSuffix != nil {
		suffix = *outDirSuffix
	}

	extractOutPath := func(file string) string {
		dir := filepath.Dir(file)
		fileName := filepath.Base(file)

		submatches := re.FindAllStringSubmatch(fileName, -1)

		out := file
		for _, subMatch := range submatches {
			for i, subExpName := range re.SubexpNames() {
				if subExpName == "language" {
					out = filepath.Join(subMatch[i], dir)
					break
				}
			}
		}
		if len(dir) > 0 && dir != "." {
			dir = dir + suffix
		}
		return filepath.Join(dir, out+".json")
	}

	type info struct {
		isStart, isEnd bool
	}

	tokens := map[string]info{
		start: {isStart: true},
		end:   {isEnd: true},
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
			logVerbosef("%v ignored", filePath)
			return nil
		}

		rel, err := filepath.Rel(rootInput, filePath)
		if err != nil {
			logErrorf("relation file %v, base %v: %v", filePath, rootInput, err)
			return nil
		}
		outFileName := extractOutPath(rel)

		raw, err := ioutil.ReadFile(path.Clean(filePath))
		if err != nil {
			logErrorf("read file %v: %v", filePath, err)
			return nil
		}

		blocks := make([]string, 0)

		errors := 0

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
				if part := content[position:nextPosition]; part == value {
					if i.isStart {
						if startBlockPos != noStart {
							errors++
							logErrorf("detected start block but previos start is not closed, position %d in '%v'",
								position, getPart(content, position))
						}
						startBlockPos = nextPosition
					} else if i.isEnd {
						if startBlockPos == noStart {
							errors++
							logErrorf("detected end block but without predefined start, position %d in '%v'",
								position, getPart(content, position))
						} else {
							blocks = append(blocks, content[startBlockPos:position])
						}
						startBlockPos = noStart
					} else {
						errors++
						logErrorf("unexpected token %v at %d", value, position)
					}
					visited = len(value)
					break
				} else {
					visited = 1
				}
			}
			position += visited
		}

		var (
			outFilePath = filepath.Join(rootOutput, outFileName)
			outFileDir  = filepath.Dir(outFilePath)
		)
		if len(blocks) == 0 {
			//do nothing
		} else if err = os.MkdirAll(outFileDir, os.ModePerm); err != nil {
			logErrorf("create output dir %v: %v", rootOutput, err)
		} else {
			if outFile, err := os.Create(outFilePath); err != nil {
				logErrorf("error of create output file %v: %v", outFilePath, err)
			} else {
				write(outFile, "{\n")

				for i, b := range blocks {
					if i > 0 {
						write(outFile, ",\n")
					}

					blockName := fmt.Sprintf("block_%d", i)
					write(outFile, fmt.Sprintf("%v\"%v\": \"%v\"", indent, blockName, processBlock(blockName, b)))
				}
				write(outFile, "\n}\n")
				_ = outFile.Sync()
				_ = outFile.Close()

				log.Printf("input %v, output %v has %d blocks, %d errors detected\n", filePath, outFilePath, len(blocks), errors)
			}

		}

		return nil
	}); err != nil {
		log.Fatalf("walkDir %v: %v", rootInput, err)
	}
}

func processBlock(name, content string) string {
	const tag = "@@"
	const tagLen = len(tag)
	out := ""
	for blockIndex := 0; ; blockIndex++ {
		boldPos := strings.Index(content, tag)
		if boldPos < 0 {
			break
		}
		nextPart := content[boldPos+tagLen:]
		finishPos := strings.Index(nextPart, tag)
		if finishPos > 0 {
			tagContent := nextPart[0:finishPos]
			out += content[0:boldPos] + fmt.Sprintf("<b class=\"%v_%d\">%v</b>", name, blockIndex, tagContent)
			content = nextPart[finishPos+tagLen:]
		}
	}
	out += content

	out = strings.ReplaceAll(out, "\\", "\\\\")
	out = strings.ReplaceAll(out, "\"", "\\\"")
	out = strings.ReplaceAll(out, "\n", "\\\\n")
	out = strings.ReplaceAll(out, "\t", "\\\\t")
	return out

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

func logErrorf(format string, args ...interface{}) {
	log.Printf("ERROR: "+format+"\n", args...)
}

func logVerbosef(format string, args ...interface{}) {
	if *verbose {
		log.Printf(format+"\n", args...)
	}
}
