package telegram

import "testing"

func TestMarkdownToHTML(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"code block", "```python\nif a < b:\n    print(a)\n```", "<pre><code class=\"language-python\">if a &lt; b:\n    print(a)</code></pre>"},
		{"unclosed block", "```\nx = 1", "<pre>x = 1</pre>"},
		{"inline code keeps stars", "use `a*b*c` here", "use <code>a*b*c</code> here"},
		{"bold italic", "**bold** and *it* and __b2__", "<b>bold</b> and <i>it</i> and <b>b2</b>"},
		{"math stars untouched", "2 * 3 * 4", "2 * 3 * 4"},
		{"snake case untouched", "my_var_name", "my_var_name"},
		{"escape", "a < b & c > d", "a &lt; b &amp; c &gt; d"},
		{"link", "[docs](https://x.io/a?b=1&c=2)", "<a href=\"https://x.io/a?b=1&amp;c=2\">docs</a>"},
		{"heading", "## Title", "<b>Title</b>"},
		{"list", "- one\n  * two", "• one\n  • two"},
		{"quote", "> a\n> b", "<blockquote>a\nb</blockquote>"},
		{"table", "| a | b |\n|---|---|\n| 1 | 2 |", "<pre>| a | b |\n|---|---|\n| 1 | 2 |</pre>"},
		{"hr", "---", "──────────"},
	}
	for _, c := range cases {
		if got := markdownToHTML(c.in); got != c.want {
			t.Errorf("%s:\n got: %q\nwant: %q", c.name, got, c.want)
		}
	}
}
