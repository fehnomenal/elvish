package newedit

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/user"
	"sync"

	"github.com/elves/elvish/eval"
	"github.com/elves/elvish/eval/vals"
	"github.com/elves/elvish/eval/vars"
	"github.com/elves/elvish/newedit/core"
	"github.com/elves/elvish/newedit/prompt"
	"github.com/elves/elvish/styled"
	"github.com/elves/elvish/util"
)

func makePrompt(ed *core.Editor, ev *eval.Evaler, ns eval.Ns, computeInit eval.Callable, name string) core.Prompt {
	compute := computeInit
	ns[name] = vars.FromPtr(&compute)
	return prompt.New(func() styled.Text {
		return callPrompt(ed, ev, compute)
	})
}

var defaultPrompt, defaultRPrompt eval.Callable

func init() {
	user, userErr := user.Current()
	isRoot := userErr == nil && user.Uid == "0"

	// Compute defaultPrompt.
	p := styled.Unstyled("> ")
	if isRoot {
		p = styled.Transform(styled.Unstyled("# "), "red")
	}
	defaultPrompt = eval.NewBuiltinFn("default prompt", func(fm *eval.Frame) {
		out := fm.OutputChan()
		out <- string(util.Getwd())
		out <- p
	})

	// Compute defaultRPrompt
	username := "???"
	if userErr == nil {
		username = user.Name
	}
	hostname, err := os.Hostname()
	if err != nil {
		hostname = "???"
	}
	rp := styled.Transform(styled.Unstyled(username+"@"+hostname), "inverse")
	defaultRPrompt = eval.NewBuiltinFn("default rprompt", func(fm *eval.Frame) {
		fm.OutputChan() <- rp
	})
}

// callPrompt calls a function with no arguments and closed input, and converts
// its outputs to styled objects. Used to call prompt callbacks.
func callPrompt(ed *core.Editor, ev *eval.Evaler, fn eval.Callable) styled.Text {
	ports := []*eval.Port{
		eval.DevNullClosedChan,
		{}, // Will be replaced when capturing output
		{File: os.Stderr},
	}

	return callForStyledText(ed, ev, fn, ports)
}

func callForStyledText(ed *core.Editor, ev *eval.Evaler, fn eval.Callable, ports []*eval.Port) styled.Text {

	var (
		result      styled.Text
		resultMutex sync.Mutex
	)
	add := func(v interface{}) {
		resultMutex.Lock()
		defer resultMutex.Unlock()
		newResult, err := result.RConcat(v)
		if err != nil {
			ed.Notify(fmt.Sprintf(
				"invalid output type from prompt: %s", vals.Kind(v)))
		} else {
			result = newResult.(styled.Text)
		}
	}

	// Value outputs are concatenated.
	valuesCb := func(ch <-chan interface{}) {
		for v := range ch {
			add(v)
		}
	}
	// Byte output is added to the prompt as a single unstyled text.
	bytesCb := func(r *os.File) {
		allBytes, err := ioutil.ReadAll(r)
		if err != nil {
			ed.Notify(fmt.Sprintf("error reading prompt byte output: %v", err))
		}
		if len(allBytes) > 0 {
			add(styled.Unstyled(string(allBytes)))
		}
	}

	// XXX There is no source to pass to NewTopEvalCtx.
	fm := eval.NewTopFrame(ev, eval.NewInternalSource("[prompt]"), ports)
	err := fm.CallWithOutputCallback(fn, nil, eval.NoOpts, valuesCb, bytesCb)

	if err != nil {
		ed.Notify(fmt.Sprintf("prompt function error: %v", err))
		return nil
	}

	return result
}