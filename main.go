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
	"strconv"
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
	langReplace  = flag.String("langReplace", "zh=ch,sv=se", "language code replacers pairs divided by comma; format: source1=replacer1,source2=replacer2")
)

func usage() {
	_, _ = fmt.Fprintf(os.Stderr, "Usage of "+name+":\n")
	_, _ = fmt.Fprintf(os.Stderr, "\t"+name+" [flags]\n")
	_, _ = fmt.Fprintf(os.Stderr, "Flags:\n")
	flag.PrintDefaults()
}

func main() {
	if err := run(); err != nil {
		log.Fatal(err.Error())
	}
}

func run() error {
	log.SetPrefix(name + ": ")

	flag.Usage = usage
	flag.Parse()

	var (
		start = *startToken
		end   = *endToken
	)
	if start == end {
		return fmt.Errorf("start block %v must be different with end one %v", start, end)
	}

	isIncluded := func(path string) bool {
		return true
	}

	if fileMatcher == nil || len(*fileMatcher) == 0 {
		return fmt.Errorf("fileMatcher regexp must be defined")
	}

	expr := *fileMatcher
	re, err := regexp.Compile(expr)
	if err != nil {
		return fmt.Errorf("invalid fileMatcher %v", expr)
	}

	langReplacers := languageReplacers()

	suffix := ""
	if outDirSuffix != nil {
		suffix = *outDirSuffix
	}

	extractOutPath := func(file string) string {
		dir := filepath.Dir(file)
		fileName := filepath.Base(file)

		noOutDir := len(dir) == 0 || dir == "."
		outFileName := dir
		if noOutDir {
			outFileName = filepath.Base(file)
			ext := filepath.Ext(file)
			if len(ext) > 0 {
				outFileName = file[:len(file)-len(ext)]
			}
		}
		submatches := re.FindAllStringSubmatch(fileName, -1)

		outFilePath := file
		for _, subMatch := range submatches {
			for i, subExpName := range re.SubexpNames() {
				if subExpName == "language" {
					lang := subMatch[i]
					if replacer, ok := langReplacers[lang]; ok {
						logVerbosef("replace lang %s by %s", lang, replacer)
						lang = replacer
					}
					outFilePath = filepath.Join(lang, outFileName)
					break
				}
			}
		}

		if !noOutDir {
			dir = dir + suffix
		}
		return filepath.Join(dir, outFilePath+".json")
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
		return fmt.Errorf("error of create input dir %v: %v", rootInput, err)
	}

	inputDirStatistic := make(map[string]int)

	if err := filepath.Walk(rootInput, func(filePath string, fileInfo fs.FileInfo, err error) error {
		if err != nil {
			return err
		} else if fileInfo.IsDir() {
			return nil
		} else if !isIncluded(filePath) {
			logVerbosef("%v ignored", filePath)
			return nil
		}

		relativeFilePath, err := filepath.Rel(rootInput, filePath)
		if err != nil {
			logErrorf("relation file %v, base %v: %v", filePath, rootInput, err)
			return nil
		}

		outFileName := extractOutPath(relativeFilePath)
		inputDir := filepath.Dir(relativeFilePath)

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
							logErrorf("detected start block but previous start is not closed, position %d in '%v'",
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
				if err:= write(outFile, "{\n"); err != nil {
					return err
				}

				numberRank := 1
				for rem := len(blocks) / 10; rem > 0; rem = rem / 10 {
					numberRank++
				}

				for i, b := range blocks {
					if i > 0 {
						if err:= write(outFile, ",\n"); err != nil {
							return err
						}
					}
					tmpl := "block_%0" + strconv.Itoa(numberRank) + "d"
					blockName := fmt.Sprintf(tmpl, i)
					if err := write(outFile, fmt.Sprintf("%v\"%v\": \"%v\"", indent, blockName, processBlock(blockName, b))); err != nil {
						return err
					}
				}
				if err := write(outFile, "\n}\n"); err != nil {
					return err
				}
				_ = outFile.Sync()
				_ = outFile.Close()

				actual := len(blocks)
				if errors > 0 {
					logInfo("input %v, output %v has %d blocks, %d errors detected\n", filePath, outFilePath, actual, errors)
				} else {
					logVerbosef("input %v, output %v has %d blocks, %d errors detected\n", filePath, outFilePath, actual, errors)
				}
				if actual > 0 {
					expected := inputDirStatistic[inputDir]
					if expected == 0 {
						inputDirStatistic[inputDir] = actual
					} else if expected != actual {
						logErrorf("blocks mismatched in %s, expected %d, actual %d", filePath, expected, actual)
						if actual > expected {
							inputDirStatistic[inputDir] = actual
						}
					}
				}

			}
		}

		return nil
	}); err != nil {
		return fmt.Errorf("walkDir %v: %v", rootInput, err)
	}
	return nil
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

func write(file *os.File, content string) error {
	if _, err := file.WriteString(content); err != nil {
		return fmt.Errorf("file %v: %w", file.Name(), err)
	}
	return nil
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

func logInfo(format string, args ...interface{}) {
	log.Printf(format+"\n", args...)
}

func languageReplacers() map[string]string {
	result := make(map[string]string)
	if langReplace == nil {
		return nil
	}
	replacers := strings.Split(*langReplace, ",")
	for _, r := range replacers {
		pair := strings.Split(r, "=")
		if len(pair) >= 2 {
			result[pair[0]] = pair[1]
		}
	}
	return result
}
