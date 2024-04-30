package main

import (
	"database/sql"
	"log"
	"os"
	"strconv"
	"strings"

	"github.com/gocolly/colly"

	"github.com/velebak/colly-sqlite3-storage/colly/sqlite3"
)

type Article struct {
	url, name, price, description string
}

type Articles map[string]Article

var articles Articles

var db *sql.DB
var tx *sql.Tx
var err error

func init_articles_db() {
	db, err = sql.Open("sqlite3", "./out.db")
	if err != nil {
		log.Fatal(err)
	}
	sqlStmt := `
	create table articles (id integer primary key, name text, price integer, url text, description text);
	`
	_, err = db.Exec(sqlStmt)
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
	}
	tx, err = db.Begin()
	if err != nil {
		log.Printf("%q: %s\n", err, sqlStmt)
	}
	log.Println("db created, ", db)
}

var collector *colly.Collector
var detailCollector *colly.Collector

func setup_collector() {
	collector = colly.NewCollector(
		colly.UserAgent("Mozilla/5.0 (X11; Linux x86_64; rv:125.0) Gecko/20100101 Firefox/125.0"),
		colly.AllowedDomains("leboncoin.fr", "www.leboncoin.fr"),
	)
	collector.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referrer", "https://www.leboncoin.fr/")
		r.Headers.Set("cookie", "__Secure-Install=0eb72874-0d14-4a5e-881b-539c63bb0bf5; datadome=vhnEwFmJknVlc4NZO3bZmHwPcu9E7d_q5Bc9tfxCXRAYntoCFX8IYHFMu3LwkDSOMV7TXDTs5CYpXhBf5KxefcLPHIgXD9PLmaZs2cFl0qMSdlYd2OhgERbrKF9_1zpM; pa_privacy=%22exempt%22; i18n_resources=0eb0,9efa,f2e2,c94c,6fff,1ea4,91bd,3495,c6e6,1296,a063,e5b0,6dd5,4a7c,55b2,edf4,7d70,5079,a7d9,32a8,a6f7,fb44,fcbb,7efc,b7d6,26d3,3ca2,8a15,ad60,9437,4880,16c7,d760,cc5f,b98f,a4a5,beeb,edd0,d405,c1bb,1920,7f31,efe2,aa75,1888,e956,68c7,bbec,a1d1,7d49,b915,d755,4638,a394,09bb,b9b3,369b,9fcf,c62f,ded9,9b11,29d1,e226,af2f,3f7e,9359,280f,ef56,978c,3541,8134; adview_clickmeter=search__listing__16__c4241319-945a-4bfb-80ee-066aa1f90c33; _dd_s=rum=0&expire=1714475545245; _pprv=eyJjb25zZW50Ijp7IjAiOnsibW9kZSI6ImVzc2VudGlhbCJ9LCI3Ijp7Im1vZGUiOiJvcHQtaW4ifX0sInB1cnBvc2VzIjpudWxsLCJfdCI6Im1iYW80c2N5fGx2bTk3YjB5In0%3D")
	})
	collector.OnResponse(func(r *colly.Response) {
		log.Println(r.Request.URL, "\t", r.StatusCode)
	})
	collector.OnError(func(r *colly.Response, err error) {
		log.Println(r.Request.URL, "\t", r.StatusCode, "\nError:", err)
	})
	collector.OnHTML("div[class^=\"styles_adCard\"]", func(h *colly.HTMLElement) {
		article_url := "https://leboncoin.fr" + h.ChildAttr("a[href]", "href")
		log.Println("new article:", article_url)
		detailCollector.Visit(article_url)
	})
}

func setup_details_collector(url string) {
	detailCollector = collector.Clone()

	detailCollector.OnRequest(func(r *colly.Request) {
		r.Headers.Set("Referrer", url)
	})
	detailCollector.OnResponse(func(r *colly.Response) {
		log.Println(r.Request.URL, "\t", r.StatusCode)
	})
	detailCollector.OnError(func(r *colly.Response, err error) {
		log.Println(r.Request.URL, "\t", r.StatusCode, "\nError:", err)
	})
	detailCollector.OnHTML("div.flex.flex-col.gap-lg", func(h *colly.HTMLElement) {
		var article = articles[h.Request.URL.String()]
		article.name = h.ChildText("h1.text-headline-1-expanded")
		article.price = h.ChildText("p.text-headline-2")
		article.url = h.Request.URL.String()
		articles[h.Request.URL.String()] = article
	})
	detailCollector.OnHTML("div>p.whitespace-pre-line", func(h *colly.HTMLElement) {
		var article = articles[h.Request.URL.String()]
		article.description = strings.ToLower(h.Text)
		articles[h.Request.URL.String()] = article
	})

}

var keywords = []string{"i7", "ryzen", "i9", "yoga", "inspiron", "lenovo", "macbook"}
var max_price = 300

func main() {
	args := os.Args
	url := args[1]
	nb_pages := 1
	if len(os.Args) > 2 {
		nb, err := strconv.Atoi(args[2])
		if err != nil {
			panic(err)
		}
		nb_pages = nb
	}
	articles = make(Articles)
	setup_collector()
	setup_details_collector(url)
	init_articles_db()

	// setup persistant cache in ./results.db
	storage := &sqlite3.Storage{
		Filename: "./results.db",
	}
	err = collector.SetStorage(storage)
	if err != nil {
		panic(err)
	}
	defer storage.Close()

	for i := 0; i < nb_pages; i++ {
		collector.Visit(url + "&page=" + strconv.Itoa(i+1))
	}
	collector.Wait()
	detailCollector.Wait()

	log.Println(db)
	queryStmt, err := db.Prepare("select url from articles where url = ?")
	if err != nil {
		log.Print(err)
	}
	defer queryStmt.Close()
	stmt, err := tx.Prepare("insert into articles(id, name, price, url, description) values(?, ?, ?, ?, ?)")
	if err != nil {
		log.Fatal(err)
	}
	defer stmt.Close()
	id := 0
	for _, value := range articles {
		var tmp Article
		log.Println("querying")
		err = queryStmt.QueryRow(value.url).Scan(&tmp)
		if err != nil {
			log.Print(err)
		} else {
			log.Println(tmp)
			continue
		}
		for _, word := range keywords {

			price, err := strconv.Atoi(value.price)
			if err != nil {
				price = 0
			}
			if (strings.Contains(value.name, word) || strings.Contains(value.description, word)) && price <= max_price {
				_, err := stmt.Exec(id, value.name, value.price, value.url, value.description)
				id += 1
				if err != nil {
					log.Fatal(err)
				}
			}

		}
	}
	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}
}
