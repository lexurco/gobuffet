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

package util

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"

	"gobuffet/util"
)

type Conf struct {
	token string
	chat  string
}

func NewConf(token string, chat int) (conf *Conf) {
	return &Conf{
		token: token,
		chat:  strconv.Itoa(chat),
	}
}

func ReadToken(file string) (token string, err error) {
	buf, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func Send(conf *Conf, msg string) (err error) {
	if conf == nil {
		return nil
	}

	url := "https://api.telegram.org/bot" + url.QueryEscape(conf.token) +
		"/sendMessage?chat_id=" + url.QueryEscape(conf.chat)

	data := map[string]string{"text": msg}
	var buf bytes.Buffer
	if err = json.NewEncoder(&buf).Encode(data); err != nil {
		util.Die(err)
	}

	resp, err := http.Post(url, "application/json", &buf)
	if err != nil {
		return err
	}

	var body struct {
		OK bool
	}
	if err = json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return err
	}

	if !body.OK {
		return errors.New("telegram API error")
	}

	return nil
}
