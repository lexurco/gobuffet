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

package serve

import (
	"bytes"
	"embed"
	"errors"
	"flag"
	"fmt"
	htemplate "html/template"
	"io"
	"log"
	"math"
	"mime"
	"mime/multipart"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"text/template"

	"golang.org/x/crypto/bcrypt"

	"github.com/jackc/pgx"

	iutil "github.com/lexurco/gobuffet/item/util"
	putil "github.com/lexurco/gobuffet/pw/util"
	tutil "github.com/lexurco/gobuffet/tg/util"
	"github.com/lexurco/gobuffet/util"
)

type price struct {
	Num int
	Str string
}

type item struct {
	ID    int
	Ord   int
	Name  string
	Descr string
	Price price
	Img   string

	Num   int
	Total price
}

var (
	errLog = log.New(os.Stderr, "", log.LstdFlags)

	flags     = flag.NewFlagSet("serve", flag.ExitOnError)
	dbFlag    = flags.String("db", "", "database connection string or URI")
	tokenFlag = flags.String("token", "", "telegram bot API token")
	chatFlag  = flags.Int("chat", math.MaxInt, "telegram bot chat ID")

	//go:embed tmpl/*.tmpl tmpl/*.htmpl
	tmplFS embed.FS
	htmpls = htemplate.Must(htemplate.ParseFS(tmplFS, "tmpl/*.htmpl"))
	tmpls  = template.Must(template.ParseFS(tmplFS, "tmpl/*.tmpl"))

	//go:embed css/*.css
	cssFS embed.FS

	dbConn *pgx.Conn
	dbLock sync.RWMutex

	intRE = regexp.MustCompile(`^0|[1-9][0-9]*$`)

	tgConf *tutil.Conf
)

func imgPath(base string) (p string) {
	return path.Clean("/" + util.ImgPath(base))
}

func getMethodLine(r *http.Request) (line string) {
	return r.Method + " " + r.URL.Path + " " + r.Proto
}

func logAccess(r *http.Request, user string, size int, status int) {
	if user == "" {
		user = "-"
	}
	log.Printf(`%v %v - %v "%v" %v %v`, r.Host, r.RemoteAddr, user,
		getMethodLine(r), status, size)
}

func logError(r *http.Request, user string, status int, err error) {
	var msg string
	if err != nil {
		msg = ": " + err.Error()
	}
	errLog.Printf(`%v %v "%v" (%v %v)%v`, r.RemoteAddr, user,
		getMethodLine(r), status, http.StatusText(status), msg)
}

func handleError(w http.ResponseWriter, r *http.Request, user string, status int, msg string) {
	if msg != "" {
		msg = ": " + msg
	}
	http.Error(w, fmt.Sprint(status, " ", http.StatusText(status), msg), status)
	logAccess(r, user, 0, status)
}

func logAndHandleError(w http.ResponseWriter, r *http.Request, user string,
	status int, msg string, err error) {

	if err != nil {
		logError(r, "", status, err)
	}
	handleError(w, r, "", status, "")
}

func getForm(w http.ResponseWriter, r *http.Request) (code int, err error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		goto ok
	}

	if r.Method != http.MethodPost {
		w.Header().Set("Allow", http.MethodPost)
		return http.StatusMethodNotAllowed,
			errors.New(r.Method + " used instead of POST")
	}
	ct, _, err = mime.ParseMediaType(ct)
	if err != nil {
		return http.StatusUnsupportedMediaType, err
	}

	switch ct {
	case "multipart/form-data":
		err = r.ParseMultipartForm(10 << 20) // 10 MiB
	case "application/x-www-form-urlencoded":
		err = r.ParseForm()
	default:
		return http.StatusUnsupportedMediaType, errors.New("bad Content-Type " + ct)
	}
	if err != nil {
		return http.StatusUnprocessableEntity, err
	}

ok:
	return http.StatusOK, nil
}

func formGetFile(w http.ResponseWriter, r *http.Request, fld string) (f multipart.File,
	fh *multipart.FileHeader, code int, err error) {

	bad := func(code int, err error) (multipart.File, *multipart.FileHeader, int, error) {
		return nil, nil, code, err
	}
	badct := func() (multipart.File, *multipart.FileHeader, int, error) {
		return bad(http.StatusBadRequest, errors.New("invalid content type of file name"))
	}

	if r.MultipartForm != nil && r.MultipartForm.File[fld] != nil {
		var err error
		f, fh, err = r.FormFile(fld)
		if err != nil {
			return bad(http.StatusBadRequest, err)
		}
	} else {
		return nil, nil, http.StatusOK, nil
	}

	hdrCT := fh.Header.Get("Content-Type")
	extCT := mime.TypeByExtension(path.Ext(fh.Filename))
	if hdrCT != extCT {
		return badct()
	}
	if typ, _, ok := strings.Cut(extCT, "/"); !ok || typ != "image" {
		return badct()
	}
	buf := make([]byte, 512)
	nbytes, err := f.Read(buf)
	if err != nil && err != io.EOF {
		return bad(http.StatusInternalServerError, err)
	}
	if _, err = f.Seek(0, 0); err != nil {
		return bad(http.StatusInternalServerError, err)
	}
	if hdrCT != http.DetectContentType(buf[:nbytes]) {
		return badct()
	}

	return f, fh, http.StatusOK, nil
}

func itemAdd(w http.ResponseWriter, r *http.Request) (code int, err error) {
	var it iutil.Item

	name := r.FormValue("name")
	if name == "" {
		return http.StatusBadRequest, errors.New("no name")
	}
	it.Name = &name

	f, fh, status, err := formGetFile(w, r, "image")
	if err != nil {
		return status, err
	}
	if f != nil {
		defer f.Close()
		it.Img.Name = &fh.Filename
		it.Img.Reader = f
	}

	descr := r.FormValue("descr")
	if descr != "" {
		it.Descr = &descr
	}

	var price int
	if err := (*iutil.Price)(&price).Set(r.FormValue("price")); err != nil {
		return http.StatusBadRequest, errors.New("invalid price")
	}
	it.Price = &price

	if err := iutil.Add(dbConn, &it); err != nil {
		return http.StatusInternalServerError, err
	}

	return http.StatusOK, nil
}

// XXX This is almost exactly the same as itemadd.
func itemMod(w http.ResponseWriter, r *http.Request) (code int, err error) {
	var it iutil.Item

	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		return http.StatusBadRequest, errors.New("bad id")
	}

	name := r.FormValue("name")
	if name != "" {
		it.Name = &name
	}

	f, fh, status, err := formGetFile(w, r, "image")
	if err != nil {
		return status, err
	}
	if f != nil {
		defer f.Close()
		it.Img.Name = &fh.Filename
		it.Img.Reader = f
	}

	descr := r.FormValue("descr")
	if descr != "" {
		it.Descr = &descr
	}

	var price int
	if s := r.FormValue("price"); s != "" {
		if err := (*iutil.Price)(&price).Set(r.FormValue("price")); err != nil {
			return http.StatusBadRequest, errors.New("invalid price")
		}
		it.Price = &price
	}

	if err := iutil.Mod(dbConn, id, "", &it); err != nil {
		return http.StatusInternalServerError, err
	}

	return http.StatusOK, nil
}

func itemDel(w http.ResponseWriter, r *http.Request) (code int, err error) {
	id, err := strconv.Atoi(r.FormValue("id"))
	if err != nil {
		return http.StatusBadRequest, errors.New("bad id")
	}
	if err = iutil.Del(dbConn, []int{id}, []string{}); err != nil {
		return http.StatusInternalServerError, err
	}
	return http.StatusOK, nil
}

func chpass(w http.ResponseWriter, r *http.Request) (code int, err error) {
	const min = 8

	pass := r.FormValue("password")
	repeat := r.FormValue("repeat")

	if len(pass) < min {
		return http.StatusOK,
			errors.New(fmt.Sprintf("password is too short (min %v characters)", min))
	}
	if pass != repeat {
		return http.StatusOK, errors.New("passwords do not match")
	}

	if err = putil.Chpass(dbConn, []byte(pass)); err != nil {
		return http.StatusInternalServerError, err
	}

	return http.StatusOK, nil
}

func setAuthHeader(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", `Basic realm="Admin Area"`)
}

func auth(w http.ResponseWriter, r *http.Request) (code int, err error) {
	var hash []byte

	u, p, ok := r.BasicAuth()
	if !ok {
		setAuthHeader(w)
		return http.StatusUnauthorized, nil
	}

	if p == "" {
		setAuthHeader(w)
		return http.StatusUnauthorized,
			errors.New("empty password login denied for " + u)
	}

	q := "SELECT pass FROM passwd WHERE name = $1"
	if err := dbConn.QueryRow(q, u).Scan(&hash); err != nil {
		if err == pgx.ErrNoRows {
			setAuthHeader(w)
			return http.StatusUnauthorized, nil
		}
		return http.StatusInternalServerError, err
	}
	if err := bcrypt.CompareHashAndPassword(hash, []byte(p)); err != nil {
		setAuthHeader(w)
		return http.StatusUnauthorized, errors.New("failed login as " + u)
	}

	return http.StatusOK, nil
}

func dbConnFix() (err error) {
	dbLock.RLock()

	if err = util.DBTest(dbConn); err != nil {
		dbLock.RUnlock()

		err = func() (err error) {
			dbLock.Lock()
			defer dbLock.Unlock()
			dbConn = nil
			if dbConn, err = util.DBConnect(*dbFlag); err != nil {
				return err
			}
			return nil
		}()
		if err != nil {
			return err
		}

		dbLock.RLock()
	}
	return nil
}

func getItems(ids []int, names []string) (items []item, err error) {
	dbItems, err := iutil.Get(dbConn, ids, names, iutil.ByName)
	if err != nil {
		return nil, err
	}

	for i := range dbItems {
		var it item
		p := &dbItems[i]
		it.ID = *p.ID
		it.Ord = i
		it.Name = *p.Name
		it.Price.Num = *p.Price
		it.Price.Str = (*iutil.Price)(p.Price).String()
		if p.Descr != nil {
			it.Descr = *p.Descr
		}
		if p.Img.Name != nil {
			it.Img = imgPath(*p.Img.Name)
		}

		items = append(items, it)
	}

	return items, nil
}

func handleAdmin(w http.ResponseWriter, r *http.Request) {
	page := struct {
		Title    string
		Currency string
		Message  string
		Items    []item
	}{
		Title:    "Rock Buffet: Admin Area",
		Currency: "GEL",
	}

	const user = "admin"

	if err := dbConnFix(); err != nil {
		logAndHandleError(w, r, "", http.StatusInternalServerError, "", err)
		return
	}
	defer dbLock.RUnlock()

	if code, err := auth(w, r); code != http.StatusOK {
		logAndHandleError(w, r, "", code, "", err)
		return
	}

	if code, err := getForm(w, r); code != http.StatusOK {
		logAndHandleError(w, r, "", code, "", err)
		return
	}

	var status int
	var err error
	if r.Method == http.MethodPost {
		action := r.FormValue("action")
		switch action {
		case "chpass":
			status, err = chpass(w, r)
		case "itemadd":
			status, err = itemAdd(w, r)
		case "itemdel":
			status, err = itemDel(w, r)
		case "itemmod":
			status, err = itemMod(w, r)
		default:
			status = http.StatusBadRequest
			err = errors.New("bad action: " + action)
		}
	}
	if err != nil {
		if status != http.StatusOK {
			logAndHandleError(w, r, user, status, "", err)
			return
		}
		page.Message = err.Error()
	}

	page.Items, err = getItems([]int{}, []string{})
	if err != nil {
		logAndHandleError(w, r, user, http.StatusInternalServerError, "", err)
		return
	}

	if err = htmpls.ExecuteTemplate(w, "admin.htmpl", page); err != nil {
		logAndHandleError(w, r, "", http.StatusInternalServerError, "", err)
	}
	logAccess(r, "", 0, http.StatusOK)
}

func stoi(s string) (n int, err error) {
	return strconv.Atoi(intRE.FindString(s))
}

func handleRoot(w http.ResponseWriter, r *http.Request) {
	var total iutil.Price
	var err error
	var ids []int
	ordered := make(map[int]int)

	const (
		actCheckout = iota
		actOrder
	)

	page := struct {
		Checkout bool
		Ordered  bool

		Title    string
		Currency string
		Delivery price
		Total    string
		Notes    []string
		Items    []item

		Name     string
		Contact  string
		Address  string
		Comments string
	}{
		Title:    "Rock Buffet",
		Currency: "GEL",
		Delivery: price{Num: 500, Str: "5.00"},
		Notes:    []string{"Diameter 30 cm", "Delivery 5 GEL"},
	}

	intErr := func(err error) {
		logAndHandleError(w, r, "", http.StatusInternalServerError, "", err)
	}

	if code, err := getForm(w, r); code != http.StatusOK {
		logAndHandleError(w, r, "", code, "", err)
		return
	}

	if r.Method == http.MethodPost {
		action := r.FormValue("action")
		switch action {
		case "order":
			page.Ordered = true
			fallthrough
		case "checkout":
			page.Checkout = true
		default:
			logAndHandleError(w, r, "", http.StatusBadRequest, "",
				errors.New("bad action: "+action))
			return
		}

		for k := range r.PostForm {
			switch k {
			case "name":
				page.Name = r.FormValue(k)
				continue
			case "contact":
				page.Contact = r.FormValue(k)
				continue
			case "address":
				page.Address = r.FormValue(k)
				continue
			case "comments":
				page.Comments = r.FormValue(k)
				continue
			}

			var id, n int
			if id, err = stoi(k); err != nil {
				continue
			}
			if n, err = stoi(r.FormValue(k)); n <= 0 || n > 100 || err != nil {
				continue
			}
			ids = append(ids, id)
			ordered[id] = n
		}
	}

	if err := dbConnFix(); err != nil {
		intErr(err)
		return
	}
	defer dbLock.RUnlock()

	page.Items, err = getItems(ids, []string{})
	if err != nil {
		intErr(err)
		return
	}

	if page.Checkout {
		for i := range page.Items {
			p := &page.Items[i]
			p.Num = ordered[p.ID]
			p.Total.Num = p.Price.Num * p.Num
			p.Total.Str = (*iutil.Price)(&p.Total.Num).String()
			total += iutil.Price(p.Total.Num)
		}
		total += iutil.Price(page.Delivery.Num)
		page.Total = total.String()

		if page.Ordered {
			var buf bytes.Buffer
			tmpls.ExecuteTemplate(&buf, "order.tmpl", page)
			tutil.Send(tgConf, string(buf.Bytes()))
		}
	}

	if err = htmpls.ExecuteTemplate(w, "root.htmpl", page); err != nil {
		intErr(err)
		return
	}
	logAccess(r, "", 0, http.StatusOK)
}

// XXX should be a way to log access
func handleStatic(w http.ResponseWriter, r *http.Request) {
	http.ServeFile(w, r, r.URL.Path[1:])
}

func handleCSS(w http.ResponseWriter, r *http.Request) {
	http.ServeFileFS(w, r, cssFS, r.URL.Path[1:])
}

func Serve(args []string) {
	var addr string
	var err error

	flags.Parse(args[1:])
	args = flags.Args()

	if *tokenFlag != "" {
		token, err := tutil.ReadToken(*tokenFlag)
		if err != nil {
			errLog.Fatal("error reading " + *tokenFlag + ": " + err.Error())
		}
		tgConf = tutil.NewConf(token, *chatFlag)
	}

	switch len(args) {
	case 0:
		addr = "127.0.0.1:8080"
	case 1:
		addr = args[0]
	default:
		util.Die("usage: " + os.Args[0] + " serve [options ...] [[network:]address]")
	}

	network := "tcp"
	if n, a, found := strings.Cut(addr, ":"); found {
		switch n {
		case "tcp", "tcp4", "tcp6", "unix", "unixpacket":
			network = n
			addr = a
		}
	}

	listener, err := net.Listen(network, addr)
	if err != nil {
		errLog.Fatal(err)
	}
	defer listener.Close()

	http.HandleFunc("/{$}", handleRoot)
	http.HandleFunc("/admin", handleAdmin)
	http.HandleFunc("GET /img/{base}", handleStatic)
	http.HandleFunc("GET /css/{base}", handleCSS)

	sigch := make(chan os.Signal, 1)
	signal.Notify(sigch, os.Interrupt, syscall.SIGTERM)

	go func() {
		log.Print("serving on " + addr)
		errLog.Fatal(http.Serve(listener, nil))
	}()

	<-sigch
}
