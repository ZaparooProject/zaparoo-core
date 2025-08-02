package parser

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/ZaparooProject/zaparoo-core/pkg/zapscript/models"
	"github.com/expr-lang/expr"
)

var (
	ErrUnexpectedEOF          = errors.New("unexpected end of file")
	ErrInvalidCmdName         = errors.New("invalid characters in command name")
	ErrInvalidAdvArgName      = errors.New("invalid characters in advanced arg name")
	ErrEmptyCmdName           = errors.New("command name is empty")
	ErrEmptyZapScript         = errors.New("script is empty")
	ErrUnmatchedQuote         = errors.New("unmatched quote")
	ErrInvalidJSON            = errors.New("invalid JSON argument")
	ErrUnmatchedInputMacroExt = errors.New("unmatched input macro extension")
	ErrUnmatchedExpression    = errors.New("unmatched expression")
	ErrBadExpressionReturn    = errors.New("expression return type not supported")
)

const (
	SymCmdStart            = '*'
	SymCmdSep              = '|'
	SymEscapeSeq           = '^'
	SymArgStart            = ':'
	SymArgSep              = ','
	SymArgDoubleQuote      = '"'
	SymArgSingleQuote      = '\''
	SymAdvArgStart         = '?'
	SymAdvArgSep           = '&'
	SymAdvArgEq            = '='
	SymJSONStart           = '{'
	SymJSONEnd             = '}'
	SymJSONEscapeSeq       = '\\'
	SymJSONString          = '"'
	SymInputMacroEscapeSeq = '\\'
	SymInputMacroExtStart  = '{'
	SymInputMacroExtEnd    = '}'
	SymExpressionStart     = '['
	SymExpressionEnd       = ']'
	TokExpStart            = "\uE000"
	TokExprEnd             = "\uE001"
)

type Command struct {
	Name    string
	Args    []string
	AdvArgs map[string]string
}

type Script struct {
	Cmds []Command
}

type PostArgPartType int

const (
	ArgPartTypeUnknown PostArgPartType = iota
	ArgPartTypeString
	ArgPartTypeExpression
)

type PostArgPart struct {
	Type  PostArgPartType
	Value string
}

func isCmdName(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '.'
}

func isAdvArgName(ch rune) bool {
	return (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') || ch == '_'
}

func isWhitespace(ch rune) bool {
	return ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r'
}

func isInputMacroCmd(name string) bool {
	switch name {
	case models.ZapScriptCmdInputKeyboard, models.ZapScriptCmdInputGamepad:
		return true
	default:
		return false
	}
}

var eof = rune(0)

type ScriptReader struct {
	r   *bufio.Reader
	pos int64
}

func NewParser(value string) *ScriptReader {
	return &ScriptReader{
		r: bufio.NewReader(bytes.NewReader([]byte(value))),
	}
}

func (sr *ScriptReader) read() (rune, error) {
	ch, _, err := sr.r.ReadRune()
	if errors.Is(err, io.EOF) {
		return eof, nil
	} else if err != nil {
		return eof, err
	}
	sr.pos++
	return ch, nil
}

func (sr *ScriptReader) unread() error {
	err := sr.r.UnreadRune()
	if err != nil {
		return err
	}
	sr.pos--
	return nil
}

func (sr *ScriptReader) peek() (rune, error) {
	for peekBytes := 4; peekBytes > 0; peekBytes-- {
		b, err := sr.r.Peek(peekBytes)
		if err == nil {
			r, _ := utf8.DecodeRune(b)
			if r == utf8.RuneError {
				return r, fmt.Errorf("rune error")
			}
			return r, nil
		}
	}
	return eof, nil
}

func (sr *ScriptReader) skip() error {
	_, err := sr.read()
	if err != nil {
		return err
	} else {
		return nil
	}
}

func (sr *ScriptReader) checkEndOfCmd(ch rune) (bool, error) {
	if ch != SymCmdSep {
		return false, nil
	}

	next, err := sr.peek()
	if err != nil {
		return false, err
	}

	switch next {
	case eof:
		return true, nil
	case SymCmdSep:
		err := sr.skip()
		if err != nil {
			return false, err
		}
		return true, nil
	default:
		return false, nil
	}
}

func (sr *ScriptReader) parseEscapeSeq() (string, error) {
	ch, err := sr.read()
	if err != nil {
		return "", err
	}
	switch ch {
	case eof:
		return "", nil
	case 'n':
		return "\n", nil
	case 'r':
		return "\r", nil
	case 't':
		return "\t", nil
	case SymEscapeSeq:
		return string(SymEscapeSeq), nil
	case SymArgDoubleQuote:
		return string(SymArgDoubleQuote), nil
	case SymArgSingleQuote:
		return string(SymArgSingleQuote), nil
	default:
		return string(ch), nil
	}
}

func (sr *ScriptReader) parseQuotedArg(start rune) (string, error) {
	arg := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return arg, err
		} else if ch == eof {
			return arg, ErrUnmatchedQuote
		}

		if ch == SymEscapeSeq {
			next, err := sr.parseEscapeSeq()
			if err != nil {
				return arg, err
			}
			arg += next
			continue
		} else if ch == SymExpressionStart {
			exprValue, err := sr.parseExpression()
			if err != nil {
				return arg, err
			}
			arg += exprValue
			continue
		}

		if ch == start {
			break
		}

		arg += string(ch)
	}

	return arg, nil
}

func (sr *ScriptReader) parseExpression() (string, error) {
	rawExpr := TokExpStart

	next, err := sr.read()
	if err != nil {
		return rawExpr, err
	} else if next != SymExpressionStart {
		err := sr.unread()
		if err != nil {
			return rawExpr, err
		}
		return string(SymExpressionStart), nil
	}

	for {
		ch, err := sr.read()
		if err != nil {
			return rawExpr, err
		} else if ch == eof {
			return rawExpr, ErrUnmatchedExpression
		}

		if ch == SymExpressionEnd {
			next, err := sr.peek()
			if err != nil {
				return rawExpr, err
			} else if next == SymExpressionEnd {
				rawExpr += TokExprEnd
				err := sr.skip()
				if err != nil {
					return rawExpr, err
				}
				break
			}
		}

		rawExpr += string(ch)
	}

	return rawExpr, nil
}

func (sr *ScriptReader) parsePostExpression() (string, error) {
	rawExpr := ""
	exprEndToken := []rune(TokExprEnd)[0]

	for {
		ch, err := sr.read()
		if err != nil {
			return rawExpr, err
		} else if ch == eof {
			return rawExpr, ErrUnmatchedExpression
		}

		if ch == exprEndToken {
			break
		}

		rawExpr += string(ch)
	}

	return rawExpr, nil
}

func (sr *ScriptReader) parseJSONArg() (string, error) {
	jsonStr := string(SymJSONStart)
	braceCount := 1
	inString := false
	escaped := false

	for braceCount > 0 {
		ch, err := sr.read()
		if err != nil {
			return "", err
		} else if ch == eof {
			return "", ErrInvalidJSON
		}

		jsonStr += string(ch)

		if escaped {
			escaped = false
			continue
		}

		if ch == SymJSONEscapeSeq {
			escaped = true
			continue
		}

		if ch == SymJSONString {
			inString = !inString
			continue
		}

		if !inString {
			switch ch {
			case SymJSONStart:
				braceCount++
			case SymJSONEnd:
				braceCount--
			}
		}
	}

	// validate json
	var jsonObj interface{}
	if err := json.Unmarshal([]byte(jsonStr), &jsonObj); err != nil {
		return "", ErrInvalidJSON
	}

	// convert back to string
	normalizedJSON, err := json.Marshal(jsonObj)
	if err != nil {
		return "", ErrInvalidJSON
	}

	return string(normalizedJSON), nil
}

func (sr *ScriptReader) parseInputMacroArg() ([]string, map[string]string, error) {
	args := make([]string, 0)
	advArgs := make(map[string]string)

	for {
		ch, err := sr.read()
		if err != nil {
			return args, advArgs, err
		} else if ch == eof {
			break
		}

		if ch == SymInputMacroEscapeSeq {
			next, err := sr.read()
			if err != nil {
				return args, advArgs, err
			} else if next == eof {
				args = append(args, string(SymInputMacroEscapeSeq))
			}

			args = append(args, string(next))
			continue
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return args, advArgs, err
		} else if eoc {
			break
		}

		if ch == SymInputMacroExtStart {
			extName := string(ch)
			for {
				next, err := sr.read()
				if err != nil {
					return args, advArgs, err
				} else if next == eof {
					return args, advArgs, ErrUnmatchedInputMacroExt
				}

				extName += string(next)

				if next == SymInputMacroExtEnd {
					break
				}
			}
			args = append(args, extName)
			continue
		} else if ch == SymAdvArgStart {
			newAdvArgs, buf, err := sr.parseAdvArgs()
			if errors.Is(err, ErrInvalidAdvArgName) {
				// if an adv arg name is invalid, fallback on treating it
				// as a list of input args
				for _, ch := range string(SymAdvArgStart) + buf {
					args = append(args, string(ch))
				}
				continue
			} else if err != nil {
				return args, advArgs, err
			}

			advArgs = newAdvArgs

			// advanced args are always the last part of a command
			break
		}

		args = append(args, string(ch))
	}

	return args, advArgs, nil
}

func (sr *ScriptReader) parseAdvArgs() (map[string]string, string, error) {
	advArgs := make(map[string]string)
	inValue := false
	currentArg := ""
	currentValue := ""
	valueStart := int64(-1)
	var buf []rune

	storeArg := func() {
		if currentArg != "" {
			currentValue = strings.TrimSpace(currentValue)
			advArgs[currentArg] = currentValue
		}
		currentArg = ""
		currentValue = ""
	}

	for {
		ch, err := sr.read()
		if err != nil {
			return advArgs, string(buf), err
		} else if ch == eof {
			break
		}

		buf = append(buf, ch)

		if inValue {
			switch {
			case valueStart == sr.pos-1 && (ch == SymArgDoubleQuote || ch == SymArgSingleQuote):
				quotedValue, err := sr.parseQuotedArg(ch)
				if err != nil {
					return advArgs, string(buf), err
				}
				currentValue = quotedValue
				continue
			case ch == SymJSONStart && valueStart == sr.pos-1:
				jsonValue, err := sr.parseJSONArg()
				if err != nil {
					return advArgs, string(buf), err
				}
				currentValue = jsonValue
				continue
			case ch == SymEscapeSeq:
				// escaping next character
				next, err := sr.parseEscapeSeq()
				if err != nil {
					return advArgs, string(buf), err
				} else if next == "" {
					currentValue += string(SymEscapeSeq)
					continue
				}
				currentValue += next
				continue
			}
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return advArgs, string(buf), err
		} else if eoc {
			break
		}

		if ch == SymAdvArgSep {
			storeArg()
			inValue = false
			continue
		} else if ch == SymAdvArgEq && !inValue {
			valueStart = sr.pos
			inValue = true
			continue
		}

		switch {
		case inValue:
			if ch == SymExpressionStart {
				exprValue, err := sr.parseExpression()
				if err != nil {
					return advArgs, string(buf), err
				}
				currentValue += exprValue
			} else {
				currentValue += string(ch)
			}
			continue
		case !isAdvArgName(ch):
			return advArgs, string(buf), ErrInvalidAdvArgName
		default:
			currentArg += string(ch)
		}
	}

	storeArg()

	return advArgs, string(buf), nil
}

func (sr *ScriptReader) parseArgs(
	prefix string,
	onlyAdvArgs bool,
	onlyOneArg bool,
) ([]string, map[string]string, error) {
	args := make([]string, 0)
	advArgs := make(map[string]string)
	currentArg := prefix
	argStart := sr.pos

argsLoop:
	for {
		ch, err := sr.read()
		if err != nil {
			return args, advArgs, err
		} else if ch == eof {
			break argsLoop
		}

		switch {
		case argStart == sr.pos-1 && (ch == SymArgDoubleQuote || ch == SymArgSingleQuote):
			quotedArg, err := sr.parseQuotedArg(ch)
			if err != nil {
				return args, advArgs, err
			}
			currentArg = quotedArg
			continue argsLoop
		case argStart == sr.pos-1 && ch == SymJSONStart:
			jsonArg, err := sr.parseJSONArg()
			if err != nil {
				return args, advArgs, err
			}
			currentArg = jsonArg
			continue argsLoop
		case ch == SymEscapeSeq:
			// escaping next character
			next, err := sr.parseEscapeSeq()
			if err != nil {
				return args, advArgs, err
			} else if next == "" {
				currentArg += string(SymEscapeSeq)
				continue argsLoop
			}
			currentArg += next
			continue argsLoop
		}

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return args, advArgs, err
		} else if eoc {
			break argsLoop
		}

		switch {
		case !onlyOneArg && ch == SymArgSep:
			// new argument
			currentArg = strings.TrimSpace(currentArg)
			args = append(args, currentArg)
			currentArg = ""
			argStart = sr.pos
			continue argsLoop
		case ch == SymAdvArgStart:
			newAdvArgs, buf, err := sr.parseAdvArgs()
			switch {
			case errors.Is(err, ErrInvalidAdvArgName):
				// if an adv arg name is invalid, fallback on treating it
				// as a positional arg with a ? in it
				currentArg += string(SymAdvArgStart) + buf
				continue argsLoop
			case err != nil:
				return args, advArgs, err
			}

			advArgs = newAdvArgs

			// advanced args are always the last part of a command
			break argsLoop
		case ch == SymExpressionStart:
			exprValue, err := sr.parseExpression()
			if err != nil {
				return args, advArgs, err
			}
			currentArg += exprValue
			continue argsLoop
		default:
			currentArg += string(ch)
			continue argsLoop
		}
	}

	if !onlyAdvArgs {
		// if a cmd was called with ":" it will always have at least 1 blank arg
		currentArg = strings.TrimSpace(currentArg)
		args = append(args, currentArg)
	}

	return args, advArgs, nil
}

func (sr *ScriptReader) parseCommand(onlyOneArg bool) (Command, string, error) {
	cmd := Command{}
	var buf []rune

commandLoop:
	for {
		ch, err := sr.read()
		if err != nil {
			return cmd, string(buf), err
		} else if ch == eof {
			break commandLoop
		}

		buf = append(buf, ch)

		eoc, err := sr.checkEndOfCmd(ch)
		if err != nil {
			return cmd, string(buf), err
		} else if eoc {
			break commandLoop
		}

		switch {
		case isCmdName(ch):
			cmd.Name += string(ch)
		case ch == SymArgStart || ch == SymAdvArgStart:
			// parse arguments
			if cmd.Name == "" {
				break commandLoop
			}

			onlyAdvArgs := false
			if ch == SymAdvArgStart {
				// roll it back to trigger adv arg parsing in parseArgs
				err := sr.unread()
				if err != nil {
					return cmd, string(buf), err
				}
				onlyAdvArgs = true
			}

			var args []string
			var advArgs map[string]string
			var err error

			if isInputMacroCmd(cmd.Name) {
				args, advArgs, err = sr.parseInputMacroArg()
				if err != nil {
					return cmd, string(buf), err
				}
			} else {
				args, advArgs, err = sr.parseArgs("", onlyAdvArgs, onlyOneArg)
				if err != nil {
					return cmd, string(buf), err
				}
			}

			if len(args) > 0 {
				cmd.Args = args
			}

			if len(advArgs) > 0 {
				cmd.AdvArgs = advArgs
			}

			break commandLoop
		default:
			// might be a launch cmd
			return cmd, string(buf), ErrInvalidCmdName
		}
	}

	if cmd.Name == "" {
		return cmd, string(buf), ErrEmptyCmdName
	}

	cmd.Name = strings.ToLower(cmd.Name)

	return cmd, string(buf), nil
}

func (sr *ScriptReader) ParseScript() (Script, error) {
	script := Script{}

	parseErr := func(err error) error {
		return fmt.Errorf("parse error at %d: %w", sr.pos, err)
	}

	parseAutoLaunchCmd := func(prefix string) error {
		args, advArgs, err := sr.parseArgs(prefix, false, true)
		if err != nil {
			return parseErr(err)
		}
		cmd := Command{
			Name: models.ZapScriptCmdLaunch,
			Args: args,
		}
		if len(advArgs) > 0 {
			cmd.AdvArgs = advArgs
		}
		script.Cmds = append(script.Cmds, cmd)
		return nil
	}

	for {
		ch, err := sr.read()
		if err != nil {
			return script, err
		} else if ch == eof {
			break
		}

		switch {
		case isWhitespace(ch):
			continue
		case sr.pos == 1 && ch == SymJSONStart:
			// reserve starting { as json script for later
			return Script{}, ErrInvalidJSON
		case ch == SymCmdStart:
			next, err := sr.peek()
			if err != nil {
				return script, parseErr(err)
			}

			switch next {
			case eof:
				return script, ErrUnexpectedEOF
			case SymCmdStart:
				err := sr.skip()
				if err != nil {
					return script, parseErr(err)
				}
			default:
				// assume it's actually an auto launch cmd
				err := parseAutoLaunchCmd("*")
				if err != nil {
					return script, parseErr(err)
				}
				continue
			}

			cmd, buf, err := sr.parseCommand(false)
			switch {
			case errors.Is(err, ErrInvalidCmdName):
				// assume it's actually an auto launch cmd
				err := parseAutoLaunchCmd("**" + buf)
				if err != nil {
					return script, parseErr(err)
				}
				continue
			case err != nil:
				return script, parseErr(err)
			default:
				script.Cmds = append(script.Cmds, cmd)
			}

			continue
		default:
			err := sr.unread()
			if err != nil {
				return script, parseErr(err)
			}

			err = parseAutoLaunchCmd("")
			if err != nil {
				return script, parseErr(err)
			}

			continue
		}
	}

	if len(script.Cmds) == 0 {
		return script, ErrEmptyZapScript
	}

	return script, nil
}

type ExprEnvDevice struct {
	Hostname string `expr:"hostname"`
	OS       string `expr:"os"`
	Arch     string `expr:"arch"`
}

type ExprEnvLastScanned struct {
	ID    string `expr:"id"`
	Value string `expr:"value"`
	Data  string `expr:"data"`
}

type ExprEnvActiveMedia struct {
	LauncherID string `expr:"launcher_id"`
	SystemID   string `expr:"system_id"`
	SystemName string `expr:"system_name"`
	Path       string `expr:"path"`
	Name       string `expr:"name"`
}

type ArgExprEnv struct {
	Platform     string             `expr:"platform"`
	Version      string             `expr:"version"`
	ScanMode     string             `expr:"scan_mode"`
	Device       ExprEnvDevice      `expr:"device"`
	LastScanned  ExprEnvLastScanned `expr:"last_scanned"`
	MediaPlaying bool               `expr:"media_playing"`
	ActiveMedia  ExprEnvActiveMedia `expr:"active_media"`
}

type CustomLauncherExprEnv struct {
	Platform  string        `expr:"platform"`
	Version   string        `expr:"version"`
	Device    ExprEnvDevice `expr:"device"`
	MediaPath string        `expr:"media_path"`
}

// ParseExpressions parses and converts expressions in the input string from
// [[...]] formatted expression fields to internal expression token delimiters,
// to be evaluated by the EvalExpressions function. This function ONLY parses
// expression symbols and escape sequences, no other ZapScript syntax.
func (sr *ScriptReader) ParseExpressions() (string, error) {
	result := ""

	for {
		ch, err := sr.read()
		if err != nil {
			return result, err
		} else if ch == eof {
			break
		}

		switch {
		case ch == SymEscapeSeq:
			next, err := sr.parseEscapeSeq()
			if err != nil {
				return result, err
			}
			result += next
			continue
		case ch == SymExpressionStart:
			exprValue, err := sr.parseExpression()
			if err != nil {
				return result, err
			}
			result += exprValue
			continue
		default:
			result += string(ch)
			continue
		}
	}

	return result, nil
}

func (sr *ScriptReader) EvalExpressions(exprEnv any) (string, error) {
	parts := make([]PostArgPart, 0)
	currentPart := PostArgPart{}

	exprStartToken := []rune(TokExpStart)[0]

	for {
		ch, err := sr.read()
		if err != nil {
			return "", err
		} else if ch == eof {
			break
		}

		if ch == exprStartToken {
			if currentPart.Type != ArgPartTypeUnknown {
				parts = append(parts, currentPart)
				currentPart = PostArgPart{}
			}

			currentPart.Type = ArgPartTypeExpression
			exprValue, err := sr.parsePostExpression()
			if err != nil {
				return "", err
			}
			currentPart.Value = exprValue

			parts = append(parts, currentPart)
			currentPart = PostArgPart{}

			continue
		} else {
			currentPart.Type = ArgPartTypeString
			currentPart.Value += string(ch)
			continue
		}
	}

	if currentPart.Type != ArgPartTypeUnknown {
		parts = append(parts, currentPart)
	}

	arg := ""
	for _, part := range parts {
		if part.Type == ArgPartTypeExpression {
			output, err := expr.Eval(part.Value, exprEnv)
			if err != nil {
				return "", err
			}

			switch v := output.(type) {
			case string:
				arg += v
			case bool:
				arg += strconv.FormatBool(v)
			case int:
				arg += strconv.Itoa(v)
			case float64:
				arg += strconv.FormatFloat(v, 'f', -1, 64)
			default:
				return "", fmt.Errorf("%w: %v (%T)", ErrBadExpressionReturn, v, v)
			}
		} else {
			arg += part.Value
		}
	}

	return arg, nil
}
