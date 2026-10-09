package domain

import "testing"

func TestExitValidation(t *testing.T) {
	cases := []struct {
		in ExitInput
		ok bool
	}{{ExitInput{Name: "日本开发", Country: "jp"}, true}, {ExitInput{Name: "", Country: "JP"}, false}, {ExitInput{Name: "a", Country: "JPN"}, false}, {ExitInput{Name: "a", Country: "JP", Port: 80}, false}, {ExitInput{Name: "a", Country: "JP", Policy: "magic"}, false}, {ExitInput{Name: "a\n", Country: "US"}, true}, {ExitInput{Name: "a\nb", Country: "US"}, false}}
	for _, c := range cases {
		err := c.in.Validate()
		if (err == nil) != c.ok {
			t.Errorf("%+v: %v", c.in, err)
		}
	}
}
