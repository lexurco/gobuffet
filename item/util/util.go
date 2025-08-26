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
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lexurco/gobuffet/util"
)

type Item struct {
	ID    *int
	Name  *string
	Descr *string
	Price *int
	Img   struct {
		Name   *string
		Reader io.Reader
	}
}

type Price int

var priceRE = regexp.MustCompile(`^([1-9][0-9]*|0)(\.[0-9][0-9]?)?$`)

func (p *Price) Set(s string) (err error) {
	match := priceRE.FindStringSubmatch(s)
	if match == nil {
		return errors.New("invalid price")
	}
	subprice := strings.Replace(match[2], ".", "", 1)
	subprice += strings.Repeat("0", 2-len(subprice))
	n, err := strconv.Atoi(match[1] + subprice)
	if err != nil {
		return err
	}
	*p = Price(n)
	return nil
}

func (p *Price) String() (s string) {
	n := int(*p)
	if n < 0 {
		n *= -1
	}

	s = strconv.Itoa(n)
	switch {
	case n < 10:
		s = "0.0" + s
	case n < 100:
		s = "0." + s
	default:
		s = s[:len(s)-2] + "." + s[len(s)-2:]
	}

	switch {
	case *p < 0:
		return "-" + s
	default:
		return s
	}
}

func ParseItem(item string) (id int, name string, err error) {
	if pre, suf, ok := strings.Cut(item, ":"); ok && pre == "name" {
		return -1, suf, nil
	} else if id, err = strconv.Atoi(item); err == nil {
		return id, "", nil
	} else if err.(*strconv.NumError).Err != strconv.ErrSyntax {
		return -1, "", err
	} else {
		return -1, item, nil
	}
}

func copyImg(name string, r io.Reader) (img string, err error) {
	img = time.Now().Format("20060102_150405") + "_" + path.Base(name)
	path := util.ImgPath(img)

	err = func() (err error) {
		w, err := os.Create(path)
		if err != nil {
			return err
		}
		defer w.Close()
		if _, err = io.Copy(w, r); err != nil {
			os.Remove(path)
			return err
		}
		return nil
	}()
	if err != nil {
		return "", err
	}
	return img, nil
}

func Add(db *pgx.Conn, it *Item) (err error) {
	var img, imgPath string
	cols := []string{"name", "price"}
	vals := []string{"$1", "$2"}
	args := []any{it.Name, it.Price}

	addArg := func(fld string, arg any) {
		cols = append(cols, fld)
		vals = append(vals, fmt.Sprintf("$%v", len(cols)))
		args = append(args, arg)
	}

	if it.Img.Reader != nil {
		img, err = copyImg(*it.Img.Name, it.Img.Reader)
		if err != nil {
			return err
		}
		imgPath = util.ImgPath(img)
		addArg("img", img)
	}
	if it.Descr != nil {
		addArg("descr", it.Descr)
	}
	_, err = db.Exec(context.Background(), fmt.Sprintf("INSERT INTO items (%v) VALUES (%v)",
		strings.Join(cols, ","), strings.Join(vals, ",")), args...)
	if err != nil {
		if img != "" {
			os.Remove(imgPath)
		}
		return err
	}

	return nil
}

func Del(db *pgx.Conn, ids []int, names []string) (err error) {
	if len(ids) == 0 && len(names) == 0 {
		return nil
	}

	var where, imgs []string
	var args []any

	newArg := func(fld string, arg any) {
		where = append(where, fmt.Sprintf("%v = $%v", fld, len(where)+1))
		args = append(args, arg)
	}

	for _, id := range ids {
		newArg("id", id)
	}
	for _, n := range names {
		newArg("name", n)
	}

	tx, err := db.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	wheres := strings.Join(where, " OR ")
	rows, err := tx.Query(context.Background(), "SELECT img FROM items WHERE "+wheres, args...)
	if err != nil && err != pgx.ErrNoRows {
		return err
	}
	for rows.Next() {
		var p *string
		if err := rows.Scan(&p); err != nil {
			return err
		}
		if p != nil {
			imgs = append(imgs, util.ImgPath(*p))
		}
	}
	_, err = tx.Exec(context.Background(), "DELETE FROM items WHERE "+wheres, args...)
	if err != nil {
		return err
	}
	tx.Commit(context.Background())

	for _, v := range imgs {
		os.Remove(v)
	}

	return nil
}

func Mod(db *pgx.Conn, id int, name string, it *Item) (err error) {
	var where, img, newImg, newImgPath string
	var set []string
	var args []any
	var whereArg any

	newArg := func(fld string, arg any) {
		set = append(set, fmt.Sprintf("%v = $%v", fld, len(set)+1))
		args = append(args, arg)
	}

	rmImg := func() {
		if newImgPath != "" {
			os.Remove(newImgPath)
		}
	}

	if it.ID != nil {
		newArg("id", *it.ID)
	}

	if it.Name != nil {
		newArg("name", *it.Name)
	}

	if it.Price != nil {
		newArg("price", it.Price)
	}

	if it.Img.Name != nil {
		if *it.Img.Name == "" {
			newArg("img", nil)
		} else {
			newImg, err = copyImg(*it.Img.Name, it.Img.Reader)
			if err != nil {
				return err
			}
			newImgPath = util.ImgPath(img)
			newArg("img", newImg)
		}
	}

	if it.Descr != nil {
		if *it.Descr == "" {
			newArg("descr", nil)
		} else {
			newArg("descr", *it.Descr)
		}
	}

	if id >= 0 {
		where = fmt.Sprintf("id = $%v", len(set)+1)
		whereArg = id
	} else {
		where = fmt.Sprintf("name = $%v", len(set)+1)
		whereArg = name
	}
	args = append(args, whereArg)

	tx, err := db.Begin(context.Background())
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())

	if it.Img.Name != nil {
		err := tx.QueryRow(context.Background(),
			"SELECT img FROM items WHERE "+where, whereArg).Scan(&img)
		if err != nil && err != pgx.ErrNoRows {
			rmImg()
			return err
		}
	}

	if _, err := tx.Exec(context.Background(), fmt.Sprintf("UPDATE items SET %v WHERE %v",
		strings.Join(set, ","), where), args...); err != nil {

		rmImg()
		return err
	}
	tx.Commit(context.Background())

	if img != "" {
		os.Remove(util.ImgPath(img))
	}

	return nil
}

type Order int

const (
	ByID Order = iota
	ByName
)

func Get(db *pgx.Conn, ids []int, names []string, ord Order) (items []Item, err error) {
	var orderBy string
	var where []string
	var args []any
	sql := "SELECT id, name, descr, price, img FROM items"

	newArg := func(fld string, arg any) {
		where = append(where, fmt.Sprintf("%v = $%v", fld, len(where)+1))
		args = append(args, arg)
	}

	for _, id := range ids {
		newArg("id", id)
	}
	for _, n := range names {
		newArg("name", n)
	}
	if len(where) > 0 {
		sql += " WHERE"
	}

	switch ord {
	case ByID:
		orderBy = "id"
	case ByName:
		orderBy = "name"
	}
	if orderBy != "" {
		orderBy = "ORDER BY " + orderBy
	}

	rows, err := db.Query(context.Background(), sql+" "+strings.Join(where, " OR ")+" "+orderBy,
		args...)
	if err != nil && err != pgx.ErrNoRows {
		return items, err
	}
	defer rows.Close()

	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Descr, &it.Price,
			&it.Img.Name); err != nil {

			return items, err
		}
		items = append(items, it)
	}
	return items, nil
}
