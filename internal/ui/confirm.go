package ui

import (
	"context"

	"github.com/skinleak/gitlume/internal/ui/theme"
)

// confirmPrompt is a yes/no question shown in the footer. The operation it
// guards runs only after an explicit yes.
type confirmPrompt struct {
	active   bool
	question string
	done     string // status line message after the operation succeeds
	op       func(context.Context) error
}

func (p *confirmPrompt) ask(question, done string, op func(context.Context) error) {
	*p = confirmPrompt{active: true, question: question, done: done, op: op}
}

func (p *confirmPrompt) close() { *p = confirmPrompt{} }

func (p *confirmPrompt) view(st theme.Styles) string {
	return st.Confirm.Render(p.question)
}
