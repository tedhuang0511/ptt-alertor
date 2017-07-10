package jobs

import (
	"fmt"
	"time"

	"strings"

	"strconv"

	log "github.com/meifamily/logrus"

	"github.com/meifamily/ptt-alertor/crawler"
	"github.com/meifamily/ptt-alertor/models/ptt/article"
	"github.com/meifamily/ptt-alertor/models/pushsum"
	"github.com/meifamily/ptt-alertor/models/subscription"
	user "github.com/meifamily/ptt-alertor/models/user/redis"
)

const stopHour = 48 * time.Hour
const checkPushSumDuration = 1 * time.Minute

var psckerCh = make(chan pushSumChecker)
var boardFinish = make(map[string]bool)

type pushSumChecker struct {
	Checker
	account string
}

func NewPushSumChecker() *pushSumChecker {
	return &pushSumChecker{}
}

func (psc pushSumChecker) String() string {
	textMap := map[string]string{
		"pushmax": "推文數",
		"pushmin": "噓文數",
	}
	subType := textMap[psc.subType]
	return fmt.Sprintf("%s@%s\r\n看板：%s；%s：%s%s", psc.word, psc.board, psc.board, subType, psc.word, psc.articles.StringWithPushSum())
}

type BoardArticles struct {
	board    string
	articles article.Articles
}

func (psc pushSumChecker) Run() {
	baCh := make(chan BoardArticles)

	go func() {
		for {
			boards := pushsum.List()
			for _, board := range boards {
				bl, ok := boardFinish[board]
				if !ok {
					boardFinish[board] = true
					ok, bl = true, true
				}
				if bl && ok {
					ba := BoardArticles{board: board}
					boardFinish[board] = false
					go psc.crawlArticles(ba, baCh)
				}
			}
			time.Sleep(checkPushSumDuration)
		}
	}()

	for {
		select {
		case ba := <-baCh:
			psc.board = ba.board
			boardFinish[ba.board] = true
			if len(ba.articles) > 0 {
				go psc.checkSubscribers(ba)
			}
		case pscker := <-psckerCh:
			ckCh <- pscker
		}
	}
}

func (psc pushSumChecker) crawlArticles(ba BoardArticles, baCh chan BoardArticles) {
	currentPage, err := crawler.CurrentPage(ba.board)
	log.Info(currentPage)
	if err != nil {
		panic(err)
	}

Page:
	for i := currentPage; ; i-- {
		articles, _ := crawler.BuildArticles(ba.board, i)
		for i := len(articles) - 1; i > 0; i-- {
			a := articles[i]
			if a.ID == 0 {
				continue
			}
			loc := time.FixedZone("CST", 8*60*60)
			t, err := time.ParseInLocation("1/02", a.Date, loc)
			now := time.Now()
			nowDate := now.Truncate(24 * time.Hour)
			t = t.AddDate(now.Year(), 0, 0)
			if err != nil {
				panic(err)
			}
			if nowDate.After(t.Add(stopHour)) {
				break Page
			}
			ba.articles = append(ba.articles, a)
		}
	}

	log.WithFields(log.Fields{
		"board": ba.board,
		"total": len(ba.articles),
	}).Info("PushSum Crawl Finish")

	baCh <- ba
}

func (psc pushSumChecker) checkSubscribers(ba BoardArticles) {
	subs := pushsum.ListSubscribers(ba.board)
	for _, account := range subs {
		u := new(user.User).Find(account)
		psc.account = u.Profile.Account
		psc.email = u.Profile.Email
		psc.line = u.Profile.Line
		psc.lineNotify = u.Profile.LineAccessToken
		psc.messenger = u.Profile.Messenger
		go psc.checkPushSum(u, ba, checkMax)
		go psc.checkPushSum(u, ba, checkMin)
	}
}

type checkPushSum func(*pushSumChecker, subscription.Subscription, article.Articles) (article.Articles, []int)

func checkMax(psc *pushSumChecker, sub subscription.Subscription, articles article.Articles) (maxArticles article.Articles, ids []int) {
	psc.word = strconv.Itoa(sub.Max)
	psc.subType = "pushmax"
	if sub.Max != 0 {
		for _, a := range articles {
			if a.PushSum >= sub.Max {
				maxArticles = append(maxArticles, a)
				ids = append(ids, a.ID)
			}
		}
	}
	return maxArticles, ids
}

func checkMin(psc *pushSumChecker, sub subscription.Subscription, articles article.Articles) (minArticles article.Articles, ids []int) {
	min := sub.Min * -1
	psc.word = strconv.Itoa(min)
	psc.subType = "pushmin"
	if sub.Min != 0 {
		for _, a := range articles {
			if a.PushSum <= min {
				minArticles = append(minArticles, a)
				ids = append(ids, a.ID)
			}
		}
	}
	return minArticles, ids
}

func (psc pushSumChecker) checkPushSum(u user.User, ba BoardArticles, checkFn checkPushSum) {
	var articles article.Articles
	var ids []int
	for _, sub := range u.Subscribes {
		if strings.EqualFold(sub.Board, ba.board) {
			articles, ids = checkFn(&psc, sub, ba.articles)
		}
	}
	if len(articles) > 0 {
		psc.articles = psc.toSendArticles(ids, articles)
		if len(psc.articles) > 0 {
			psckerCh <- psc
		}
	}
}

func (psc pushSumChecker) toSendArticles(ids []int, articles article.Articles) article.Articles {
	kindMap := map[string]string{
		"pushmin": "min",
		"pushmax": "max",
	}
	ids = pushsum.DiffList(psc.account, psc.board, kindMap[psc.subType], ids...)
	diffIds := make(map[int]bool)
	for _, id := range ids {
		diffIds[id] = true
	}
	sendArticles := make(article.Articles, 0)
	for _, a := range articles {
		if diffIds[a.ID] {
			sendArticles = append(sendArticles, a)
		}
	}
	return sendArticles
}
