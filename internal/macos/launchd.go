package macos

import (
	"bytes"
	"encoding/xml"
	"sort"
	"strings"
)

func xmlString(s string) string {
	var out bytes.Buffer
	_ = xml.EscapeText(&out, []byte(s))
	return out.String()
}

// LaunchdJob is a launchd job as dockhand writes one: what it runs, with
// its environment, and the log its output and errors share. Every value is
// escaped, keys as well as strings.
type LaunchdJob struct {
	Label       string
	Arguments   []string
	Log         string
	Environment map[string]string
	// KeepAlive restarts the job whenever it ends; a one-shot job's is
	// false.
	KeepAlive bool
	// KeepAliveWhile, a path, narrows KeepAlive to while that path exists
	// (launchd.plist's PathState): an agent whose program is uninstalled
	// isn't restarted, which left launchd spawning a missing program in a
	// loop, exit 78 (the rc6 full stage, A8).
	KeepAliveWhile string
	// ProcessType is launchd's, such as Background; none where empty.
	ProcessType string
}

// LaunchdPlist describes a one-shot service with a shared stdout/stderr log.
func LaunchdPlist(label string, args []string, log string, env map[string]string) []byte {
	return LaunchdJob{Label: label, Arguments: args, Log: log, Environment: env}.Plist()
}

// Plist is the job's property list, the one way dockhand writes one.
func (j LaunchdJob) Plist() []byte {
	var out strings.Builder
	out.WriteString(`<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>Label</key><string>` + xmlString(j.Label) + `</string><key>ProgramArguments</key><array>`)
	for _, arg := range j.Arguments {
		out.WriteString("<string>" + xmlString(arg) + "</string>")
	}
	keepAlive := "<false/>"
	switch {
	case j.KeepAlive && j.KeepAliveWhile != "":
		keepAlive = "<dict><key>PathState</key><dict><key>" + xmlString(j.KeepAliveWhile) + "</key><true/></dict></dict>"
	case j.KeepAlive:
		keepAlive = "<true/>"
	}
	out.WriteString(`</array><key>RunAtLoad</key><true/><key>KeepAlive</key>` + keepAlive + `<key>AbandonProcessGroup</key><false/>`)
	if j.ProcessType != "" {
		out.WriteString(`<key>ProcessType</key><string>` + xmlString(j.ProcessType) + `</string>`)
	}
	out.WriteString(`<key>StandardOutPath</key><string>` + xmlString(j.Log) + `</string><key>StandardErrorPath</key><string>` + xmlString(j.Log) + `</string><key>EnvironmentVariables</key><dict>`)
	keys := make([]string, 0, len(j.Environment))
	for key := range j.Environment {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		out.WriteString("<key>" + xmlString(key) + "</key><string>" + xmlString(j.Environment[key]) + "</string>")
	}
	out.WriteString("</dict></dict></plist>")
	return []byte(out.String())
}

// ReadLaunchdJob reads a property list Plist wrote back into its job: the
// label, the arguments, the log, the environment, whether it's kept
// alive and while what path, and its process type. A list another
// program wrote may read in part; one that isn't a property list is an
// error.
func ReadLaunchdJob(data []byte) (LaunchdJob, error) {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.Strict = false
	var root plistValue
	for {
		token, err := decoder.Token()
		if err != nil {
			return LaunchdJob{}, err
		}
		if start, ok := token.(xml.StartElement); ok && start.Name.Local == "plist" {
			if root, err = readPlistValue(decoder); err != nil {
				return LaunchdJob{}, err
			}
			break
		}
	}
	job := LaunchdJob{Environment: map[string]string{}}
	dict := root.dict
	job.Label = dict["Label"].text
	for _, arg := range dict["ProgramArguments"].array {
		job.Arguments = append(job.Arguments, arg.text)
	}
	job.Log = dict["StandardOutPath"].text
	job.ProcessType = dict["ProcessType"].text
	for key, value := range dict["EnvironmentVariables"].dict {
		job.Environment[key] = value.text
	}
	switch alive := dict["KeepAlive"]; {
	case alive.kind == "true":
		job.KeepAlive = true
	case alive.kind == "dict":
		job.KeepAlive = true
		for path := range alive.dict["PathState"].dict {
			job.KeepAliveWhile = path
		}
	}
	return job, nil
}

// plistValue is one value of a property list: a string's text, true or
// false, an array, or a dict.
type plistValue struct {
	kind  string
	text  string
	array []plistValue
	dict  map[string]plistValue
}

// readPlistValue reads the next value, its start element the next token.
func readPlistValue(decoder *xml.Decoder) (plistValue, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return plistValue{}, err
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		value := plistValue{kind: start.Name.Local}
		switch start.Name.Local {
		case "dict", "array":
			return readNested(decoder, start)
		case "true", "false":
			return value, decoder.Skip()
		default:
			var text string
			if err := decoder.DecodeElement(&text, &start); err != nil {
				return value, err
			}
			value.text = text
			return value, nil
		}
	}
}

// readPlistKey reads a dict's next key; done at the dict's end.
func readPlistKey(decoder *xml.Decoder) (string, bool, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", false, err
		}
		switch t := token.(type) {
		case xml.EndElement:
			return "", true, nil
		case xml.StartElement:
			var key string
			err := decoder.DecodeElement(&key, &t)
			return key, false, err
		}
	}
}

// readPlistItem reads an array's next value; done at the array's end.
func readPlistItem(decoder *xml.Decoder) (plistValue, bool, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return plistValue{}, false, err
		}
		switch t := token.(type) {
		case xml.EndElement:
			return plistValue{}, true, nil
		case xml.StartElement:
			value := plistValue{kind: t.Name.Local}
			switch t.Name.Local {
			case "dict", "array":
				// Read from its start, as readPlistValue does.
				nested, err := readNested(decoder, t)
				return nested, false, err
			case "true", "false":
				return value, false, decoder.Skip()
			default:
				var text string
				err := decoder.DecodeElement(&text, &t)
				value.text = text
				return value, false, err
			}
		}
	}
}

// readNested reads a dict or array whose start element has been read.
func readNested(decoder *xml.Decoder, start xml.StartElement) (plistValue, error) {
	value := plistValue{kind: start.Name.Local}
	if start.Name.Local == "dict" {
		value.dict = map[string]plistValue{}
		for {
			key, done, err := readPlistKey(decoder)
			if err != nil || done {
				return value, err
			}
			if value.dict[key], err = readPlistValue(decoder); err != nil {
				return value, err
			}
		}
	}
	for {
		item, done, err := readPlistItem(decoder)
		if err != nil || done {
			return value, err
		}
		value.array = append(value.array, item)
	}
}
