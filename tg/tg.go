// COPYRIGHT (c) 2025 Eneik
//
// Permission to use, copy, modify, and distribute this software for any
// purpose with or without fee is hereby granted, provided that the above
// copyright notice and this permission notice appear in all copies.
//
// THE SOFTWARE IS PROVIDED "AS IS" AND THE AUTHOR DISCLAIMS ALL WARRANTIES
// WITH REGARD TO THIS SOFTWARE INCLUDING ALL IMPLIED WARRANTIES OF
// MERCHANTABILITY AND FITNESS. IN NO EVENT SHALL THE AUTHOR BE LIABLE FOR
// ANY SPECIAL, DIRECT, INDIRECT, OR CONSEQUENTIAL DAMAGES OR ANY DAMAGES
// WHATSOEVER RESULTING FROM LOSS OF USE, DATA OR PROFITS, WHETHER IN AN
// ACTION OF CONTRACT, NEGLIGENCE OR OTHER TORTIOUS ACTION, ARISING OUT OF
// OR IN CONNECTION WITH THE USE OR PERFORMANCE OF THIS SOFTWARE.

package tg

import (
	"flag"
	"io"
	"math"
	"os"

	tutil "gobuffet/tg/util"
	"gobuffet/util"
)

var flags = flag.NewFlagSet(os.Args[0]+" tg", flag.ExitOnError)
var tokenFlag = flags.String("token", "", "file containing the API token")
var chatFlag = flags.Int("chat", math.MaxInt, "chat ID")

func Tg(args []string) {
	var msg string

	flags.Parse(args[1:])
	args = flags.Args()

	if *tokenFlag == "" {
		util.Die("token file be empty")
	}
	if *chatFlag == math.MaxInt {
		util.Die("please provide the chat id")
	}

	token, err := tutil.ReadToken(*tokenFlag)
	if err != nil {
		util.Die("error reading " + *tokenFlag + ": " + err.Error())
	}
	conf := tutil.NewConf(token, *chatFlag)

	switch len(args) {
	case 0:
		buf, err := io.ReadAll(os.Stdin)
		if err != nil {
			util.Die(err)
		}
		msg = string(buf)
	case 1:
		msg = args[0]
	default:
		util.Die("usage: " + flags.Name() + " [option ...] [message]")
	}

	if err = tutil.Send(conf, msg); err != nil {
		util.Die(err)
	}
}
