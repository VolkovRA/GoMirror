package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

// Статус сканера.
// Описывает внутреннее состояние сканера, ошибки.
type ScannerState int

// Получить текстовое представление статуса сканера.
func (v ScannerState) String() string {
	switch v {
	case ScannerReady:
		return "Готов"
	case ScannerPreparing:
		return "Подготовка"
	case ScannerIncorrectURL:
		return "Ошибка: Некорректный URL"
	case ScannerOutputDirExist:
		return "Ошибка: Папка для этого сайта уже существует"
	case ScannerOutputDirError:
		return "Ошибка: Не удалось создать папку для сохранения сайта"
	case ScannerScanning:
		return "Сканирование"
	case ScannerComplete:
		return "Сканирование завершено"
	default:
		return "Unknown"
	}
}

const (

	// Готов к запуску
	ScannerReady ScannerState = iota

	// Указан некорректный URL для сканирования.
	// Конечное состояние сканера, когда невозможно начать
	// сканирование из-за ошибки анализатора URL.
	//
	// Вы можете попробовать запустить сканер повторно
	// исправив URL.
	ScannerIncorrectURL

	// Подготовка сканера к запуску.
	// Сюда входит:
	//   - Парсинг введённого URL;
	//   - Проверка наличия папки для сохранения файлов в OS;
	//   - Удаление старых файлов от предыдущего сканирования,
	//     если сканер запущен в режиме перезаписи (replaceOutDir=true).
	ScannerPreparing

	// Папка для сохранения файлов уже существует.
	// Конечное состояние сканера, когда папка для записи
	// данных сайта уже существует и сканер был запущен
	// без режима перезаписи файлов (replaceOutDir=false).
	//
	// Вы можете запустить сканер повторно указав режим
	// перезаписи данных (replaceOutDir=true). Это удалит
	// все данные от предыдущего сканирования, или вы можете
	// запустить сканер указав другой URL.
	ScannerOutputDirExist

	// Не удалось создать папку для файлов
	// Конечное состояние сканера. Вы можете запустить сканер повторно.
	ScannerOutputDirError

	// Выполняется сканирование.
	// Сюда входит:
	//   - Выполнение запроса по указанному URL;
	//   - Анализ полученного документа и поиск других ссылок;
	//   - Выполнение запросов по всем найденным, интересным ссылкам;
	//   - Сохранение всех полученных, интересных данных в папку.
	//
	// Процесс выполняется до тех пор, пока все найденные ссылки не
	// будут просканированы. После завершения работы сканер переходит
	// в статус ScannerComplete.
	ScannerScanning

	// Готово
	ScannerComplete
)

// Параметры для запуска сканера
type ScannerParams struct {

	// Исходный URL, заданный пользователем при сканирований.
	URL string

	// Удалить все старые данные от предыдущего сканирования,
	// если они имеются.
	ReplaceOutDir bool

	// Максимальное кол-во повторных попыток запроса, не считая
	// первый запрос:
	//   * 0 - Без повторных попыток, только один запрос;
	//   * 2 - Две повторные попытки в случае ошибки.
	//
	// Обратите внимание, что для некоторых http кодов не действует
	// ограничение на максимальное кол-во попыток запроса, они
	// запрашиваются бесконечное кол-во раз, пока не будет получен
	// ответ, отличный от этих кодов:
	//   * 503 Превышение кол-ва запросов. (Рано или поздно сервер сдастся)
	RepeatsMax int
}

// Сканер сайта
type Scanner struct {
	mu         sync.RWMutex
	workers    sync.WaitGroup // Синхронизация всех запросов
	params     ScannerParams  // Параметры
	state      ScannerState   // Состояние сканера
	limiter    chan int8      // Ограничитель кол-ва параллельных запросов
	sources    *Sources       // Список всех найденных и обрабатываемых ресурсов
	url        *url.URL       // Распарсенный адрес исходного URL для внутренней работы
	home       string         // Домашний каталог
	dir        string         // Папка для сохранения ресурсов
	dateStart  time.Time      // Дата запуска для статистики
	dateScan   time.Time      // Дата первого запроса для статистики
	dateFinish time.Time      // Дата завершения обработки для статистики
	err        error          // Ошибка при работе сканера
	threads    int            // Колв-во активных горутин
}

// Создать новый сканер
func NewScanner() *Scanner {
	return &Scanner{}
}

// Сбросить сканер для новой работы
func (s *Scanner) reset() *Scanner {
	s.sources = newSources(s)
	s.limiter = make(chan int8, PARALLEL_REQUESTS_MAX)
	s.dateStart = time.Time{}
	s.dateScan = time.Time{}
	s.dateFinish = time.Time{}
	s.state = ScannerReady
	s.params = ScannerParams{}
	s.url = nil
	s.dir = ""
	s.err = nil
	s.threads = 0
	return s
}

// Запустить сканер.
// Это асинхронный вызов. Чтобы дождаться завершения работы
// или для проверки текущего состояния работы сканера вы
// можете периодически опрашивать свойство: Scanner.State().
//
// Ошибка возвращается, если сканер попытаться запустить из
// не конечных состояний:
//   * ScannerStatePreparing - сканер выполняет подготовку,
//     дождитесь завершения;
//   * ScannerScanning - сканирование уже выполняется,
//     дождитесь завершения
// Остальные состояния сканера являются конечными и для них
// может быть выполнен запуск.
func (s *Scanner) Start(params ScannerParams) error {

	// Запуск:
	s.mu.Lock()
	switch s.state {
	case ScannerReady, ScannerOutputDirExist, ScannerIncorrectURL, ScannerComplete:
		s.reset()
		s.dateStart = time.Now()
		s.state = ScannerPreparing
		s.params = params
		s.mu.Unlock()
	default:
		v := s.state.String()
		s.mu.Unlock()
		return fmt.Errorf("Нельзя запустить работающий сканер, когда он в режиме: \"%v\"", v)
	}

	// Главный поток сканера:
	var work = func() {
		var err error

		// Анализ URL:
		s.mu.Lock()
		s.url, err = s.parseURL(params.URL)
		if err != nil {
			s.state = ScannerIncorrectURL
			s.err = fmt.Errorf("Не удалось запустить сканер из-за ошибки разбора URL: %w", err)
			s.dateFinish = time.Now()
			s.mu.Unlock()
			return
		}
		s.mu.Unlock()

		// Получение пути для вывода:
		s.mu.Lock()
		s.home, err = s.binPath()
		if err != nil {
			s.state = ScannerOutputDirError
			s.err = err
			s.dateFinish = time.Now()
			s.mu.Unlock()
			return
		}
		s.dir = s.home + string(os.PathSeparator) + s.url.Host
		s.mu.Unlock()

		// Создание папки:
		s.mu.Lock()
		file, err := os.Stat(s.dir)
		if err != nil {
			if os.IsNotExist(err) {
				// Создаём новую папку:
				if err2 := os.Mkdir(s.dir, 0777); err2 != nil {
					s.err = fmt.Errorf("Не удалось создать папку для данных сайта: %w", err2)
					s.state = ScannerOutputDirError
					s.dateFinish = time.Now()
					s.mu.Unlock()
					return
				}
			} else {
				// Папка есть а доступа к ней нет:
				s.err = fmt.Errorf("Ошибка доступа к папке для сохранения: %w", err)
				s.state = ScannerOutputDirError
				s.dateFinish = time.Now()
				s.mu.Unlock()
				return
			}
		} else {
			if file.IsDir() {
				// Папка уже существует:
				if s.params.ReplaceOutDir {
					if err = os.RemoveAll(s.dir); err != nil {
						s.err = fmt.Errorf("Не удалось удалить старую папку с данными сайта: \"%v\": %w", s.dir, err)
						s.state = ScannerOutputDirError
						s.dateFinish = time.Now()
						s.mu.Unlock()
						return
					}

					// Создаём новую:
					if err = os.Mkdir(s.dir, 0777); err != nil {
						s.err = fmt.Errorf("Не удалось создать новую папку для данных сайта: \"%v\": %w", s.dir, err)
						s.state = ScannerOutputDirError
						s.dateFinish = time.Now()
						s.mu.Unlock()
						return
					}

				} else {
					s.err = fmt.Errorf("Папка для данных сайта уже существует, сперва удалите её: \"%v\"", s.dir)
					s.state = ScannerOutputDirExist
					s.dateFinish = time.Now()
					s.mu.Unlock()
					return
				}
			} else {
				// Тут лежит какойто файл:
				s.err = fmt.Errorf("Ошибка, путь для создания папки с данными сайта занят файлом: \"%v\"", s.dir)
				s.state = ScannerOutputDirError
				s.dateFinish = time.Now()
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()

		// Создание файла журнала:
		s.mu.Lock()
		p := path.Clean(s.home + string(os.PathSeparator) + s.url.Host + ".log")
		os.Truncate(p, 0)
		f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0777)
		defer f.Close()
		if err != nil {
			s.err = fmt.Errorf("Ошибка, не удалось создать файл для вывода логов: \"%v\"", p)
			s.state = ScannerOutputDirError
			s.dateFinish = time.Now()
			s.mu.Unlock()
			return
		}
		log.SetOutput(f)
		s.mu.Unlock()

		// Запуск сканирования:
		s.mu.Lock()
		s.state = ScannerScanning
		s.dateScan = time.Now()
		s.mu.Unlock()

		s.workers.Add(3)
		go s.scan(s.url)
		go s.scan(s.rootFile(s.url, "/robots.txt"))
		go s.scan(s.rootFile(s.url, "/sitemap.xml"))

		// Ожидание завершения всех потоков:
		s.workers.Wait()
		log.Println("\n\nПолный отчёт сканирования:\n" + scanner.Report(true))

		s.mu.Lock()
		s.state = ScannerComplete
		s.dateFinish = time.Now()
		s.mu.Unlock()
	}
	go work()
	return nil
}

// Получить путь для корневого файла, такого как: robots.txt, ...
func (s *Scanner) rootFile(base *url.URL, file string) *url.URL {
	u2, _ := url.Parse(base.String())
	u2.Path = file
	return u2
}

// Сканирование URL в отдельном потоке
func (s *Scanner) scan(url *url.URL) {
	defer s.workers.Done()
	defer func() {
		s.mu.Lock()
		s.threads--
		s.mu.Unlock()
	}()
	if url == nil {
		return
	}

	// Счётчик горутин:
	s.mu.Lock()
	s.threads++
	s.mu.Unlock()

	// Добавляем ресурс:
	obj, ok := s.sources.Add(url)
	if ok == false {
		return
	}

	// Пропуск слишком длинных URL: (Иногда туда попадают куски двоичных данных)
	if len(url.String()) > 1000 {
		obj.mu.Lock()
		obj.state = SourceSkip
		obj.err = fmt.Errorf("Пропуск ссылки (Слишком длинная): " + string([]rune(url.String())[0:80]) + "...")
		log.Printf(obj.err.Error())
		obj.mu.Unlock()
		return
	}

	// Логируем ссылку:
	log.Println("Новая ссылка: " + url.String())

	// Пропуск не интересных ресурсов - телефоны, почты, фтп и т.д.:
	obj.mu.Lock()
	if obj.isInteresting == false {
		obj.state = SourceSkip
		obj.mu.Unlock()
		log.Println("Пропуск ссылки (Не интересная): " + url.String())
		return
	}
	obj.mu.Unlock()

	// Пропуск внешних ресурсов:
	obj.mu.Lock()
	if obj.isExternal {
		obj.state = SourceSkip
		obj.mu.Unlock()
		log.Printf("Пропуск ссылки (Внешняя): %v\n", url.String())
		return
	}
	obj.mu.Unlock()

	// Ресурс ранее не обрабатывался
	// Ожидаем нашу очередь на запрос:
	s.limiter <- 0

	// Запрос ресурса:
	var body []byte
	for {
		obj.mu.Lock()
		obj.state = SourceRequest
		obj.mu.Unlock()

		resp, err := http.Get(url.String())

		// Сетевая ошибка:
		if err != nil {
			obj.mu.Lock()
			obj.repeats++

			if obj.repeats > s.params.RepeatsMax {
				// Попытки запросов закончились:
				obj.state = SourceRequestError
				obj.err = err
				obj.mu.Unlock()

				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				<-s.limiter
				log.Println("Пропуск ссылки (Исчерпан лимит попыток запроса): " + url.String())
				return
			} else {
				// Повтор попытки:
				obj.state = SourceRequestWaitRepeat
				obj.err = err
				obj.mu.Unlock()

				if resp != nil && resp.Body != nil {
					resp.Body.Close()
				}
				continue
			}
		}

		// Обработка некоторых HTTP кодов
		// Превышение кол-ва запросов:
		if resp.StatusCode == 503 {
			obj.mu.Lock()
			obj.state = SourceRequestWaitRepeat
			obj.err = fmt.Errorf(resp.Status)
			obj.repeats++
			try := obj.repeats
			obj.mu.Unlock()

			resp.Body.Close()
			s.waitRepeat(try)
			continue
		}

		// Любые ошибки:
		if resp.StatusCode >= 400 {
			obj.mu.Lock()
			obj.state = SourceRequestError
			obj.err = fmt.Errorf(resp.Status)
			obj.mu.Unlock()

			resp.Body.Close()
			<-s.limiter
			log.Printf("Пропуск ссылки (%v): %v\n", resp.Status, url.String())
			return
		}

		// Заголовки:
		obj.mu.Lock()
		if resp.ContentLength > 0 {
			obj.size = resp.ContentLength
		}
		obj.state = SourceDownload
		obj.mu.Unlock()

		// Скачиваем всё тело:
		body, err = ioutil.ReadAll(resp.Body)
		resp.Body.Close()
		obj.mu.Lock()
		obj.size = int64(len(body))
		if err != nil {
			obj.err = err
			obj.repeats++
			if obj.repeats > s.params.RepeatsMax {
				obj.state = SourceDownloadError
				obj.mu.Unlock()
				<-s.limiter
				log.Printf("Пропуск ссылки (Ошибка сачивания тела, исчерпаны попытки): %v, %v\n", url.String(), err.Error())
				return
			} else {
				obj.state = SourceRequest
				obj.mu.Unlock()
				continue
			}
		}
		obj.mu.Unlock()
		break
	}
	<-s.limiter

	// Читаем тело, ищем доп. ссылки и запускаем параллельные сканирования:
	obj.mu.Lock()
	obj.state = SourceRead
	obj.mu.Unlock()

	// Определяем mime тип ресурса, запускаем анализ тела для поиска ссылок:
	mim := http.DetectContentType(body)
	obj.mu.Lock()
	obj.mime = mim
	obj.mu.Unlock()

	// All mime types:
	// https://www.iana.org/assignments/media-types/media-types.xhtml
	if strings.Contains(mim, "text/html") {
		s.readHTML(obj, body)
		s.readTXT(obj, body)
	} else if strings.Contains(mim, "application/octet-stream") ||
		strings.Contains(mim, "model") ||
		strings.Contains(mim, "font") ||
		strings.Contains(mim, "image") ||
		strings.Contains(mim, "video") ||
		strings.Contains(mim, "audio") ||
		strings.Contains(mim, "application/ogg") {
		// Игнорируем анализ этих типов..
	} else {
		s.readTXT(obj, body)
	}

	obj.mu.Lock()
	obj.state = SourceSave
	obj.mu.Unlock()

	// Получаем путь и имя файла для записи файла на диск:
	path, name := filepath.Split(obj.url.Path)
	if name == "" {
		name = "/index.html"
	} else if filepath.Ext(name) == "" {
		s, _ := mime.ExtensionsByType(mim)
		if len(s) == 0 {
			name += ".html"
		} else {
			name += s[0]
		}
	}

	// Из-за возможных ошибок анализа файл не должен быть выше корневой директорий или не в ней:
	if err := s.isParentPath(s.dir, s.dir+path+name); err != nil {
		obj.mu.Lock()
		obj.state = SourceSaveError
		obj.err = err
		obj.mu.Unlock()
		log.Printf("Пропуск ссылки (Некорректный путь для сохранения файла): %v, %v\n", url.String(), err.Error())
		return
	}

	// Создаём путь:
	if err := os.MkdirAll(s.dir+path, 0777); err != nil {
		obj.mu.Lock()
		obj.state = SourceSaveError
		obj.err = err
		obj.mu.Unlock()
		log.Printf("Пропуск ссылки (Не удалось создать путь для сохранения файла в системе): %v, %v\n", url.String(), err.Error())
		return
	}

	// Пишем файл:
	if err := os.WriteFile(s.dir+path+name, body, 0777); err != nil {
		obj.mu.Lock()
		obj.state = SourceSaveError
		obj.err = err
		obj.mu.Unlock()
		log.Printf("Пропуск ссылки (Не удалось сохранить файл): %v, %v\n", url.String(), err.Error())
		return
	}

	// Ресурс успешно обработан:
	obj.mu.Lock()
	obj.state = SourceComplete
	obj.mu.Unlock()
}

func (s *Scanner) isParentPath(parent string, child string) error {
	p := strings.Split(filepath.Clean(parent), string(os.PathSeparator))
	c := strings.Split(filepath.Clean(child), string(os.PathSeparator))

	if len(p) == 0 || len(c) == 0 {
		return fmt.Errorf("Получен некорректный путь файла: \"%v\" для записи в: \"%v\"", child, parent)
	}
	if len(c) < len(p) {
		return fmt.Errorf("Получен некорректный путь файла: \"%v\" для записи в: \"%v\" - дочерний путь короче родительского", child, parent)
	}
	log.Println(p, c)
	for i := range p {
		if p[i] != c[i] {
			return fmt.Errorf("Получен некорректный путь файла: \"%v\" для записи в: \"%v\" - файл не в родительском каталоге", filepath.Clean(child), filepath.Clean(parent))
		}
	}

	return nil
}

// Ждать следующую попытку
func (s *Scanner) waitRepeat(try int) {
	if try < 1 {
		return
	}

	switch try {
	case 1:
		time.Sleep(time.Millisecond * 200)
	case 2:
		time.Sleep(time.Millisecond * 500)
	case 3:
		time.Sleep(time.Millisecond * 1000)
	case 4:
		time.Sleep(time.Millisecond * 2000)
	case 5:
		time.Sleep(time.Millisecond * 5000)
	default:
		time.Sleep(time.Millisecond * 8000)
	}
}

// Прочитать тело файла для поиска и сканирования других ссылок
func (s *Scanner) readHTML(obj *Source, body []byte) {
	doc, err := html.Parse(bytes.NewReader(body))
	if err != nil {
		obj.mu.Lock()
		obj.errRead = err
		obj.mu.Unlock()
		return
	}

	// Проходим по всем тегам:
	var f func(n *html.Node)
	f = func(n *html.Node) {
		for i := range n.Attr {
			a := &n.Attr[i]

			// Ищем любые ссылки в теге:
			var links []*url.URL
			switch a.Key {
			case "src", "href":
				links = s.parseSrc(n, a)
			case "srcset", "data-srcset":
				links = s.parseSrcset(n, a)
			}

			// Запуск сканирования всех найденных ссылок:
			if len(links) > 0 {
				for j := 0; j < len(links); j++ {
					s.workers.Add(1)
					go s.scan(links[j])
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			f(c)
		}
	}
	f(doc)
}

// Обработать ссылки в атрибуте "src" любого тега
func (s *Scanner) parseSrc(n *html.Node, a *html.Attribute) []*url.URL {
	u, e := url.Parse(a.Val)
	if e != nil {
		log.Printf("Ошибка разбора src ссылки в теге: <%v ... %v=\"%v\" ... >: %v", n.Data, a.Key, a.Val, e.Error())
		return nil
	}

	// Относительные ссылки в абсолютные, чтоб программа могла
	// сравнить домен ссылки с родным: (Только внутри программы)
	if u.IsAbs() == false {
		u.Scheme = s.url.Scheme
		u.Host = s.url.Host
	}

	return []*url.URL{u}
}

// Обработать ссылки в атрибуте "srcset" любого тега
func (s *Scanner) parseSrcset(n *html.Node, a *html.Attribute) []*url.URL {
	arr := strings.Split(a.Val, ",")
	res := make([]*url.URL, 0, len(arr))
	for i := 0; i < len(arr); i++ {
		arr2 := strings.Split(arr[i], " ")
		for j := 0; j < len(arr2); j++ {
			u, e := url.Parse(arr[i])
			if e != nil {
				log.Printf("Ошибка разбора %v-ого значения атрибута в srcset ссылке тега: <%v ... %v=\"%v\" ... >: %v", i, n.Data, a.Key, a.Val, e.Error())
				continue
			}

			// Относительные ссылки в абсолютные, чтоб программа могла
			// сравнить домен ссылки с родным: (Только внутри программы)
			if u.IsAbs() == false {
				u.Scheme = s.url.Scheme
				u.Host = s.url.Host
			}

			res = append(res, u)
		}
	}

	return res
}

func (s *Scanner) readTXT(obj *Source, body []byte) {

	// Шаблоны для CSS:
	// url(...)
	// URL(...)
	// url('...')
	// url("...")
	// url(`...`)
	// url (...)
	// url  (...)
	reg := regexp.MustCompile(`(?i)url *\(`)
	res := reg.FindAllIndex(body, -1)
	for i := 0; i < len(res); i++ {
		if url := s.searchLink(body, res[i][1]); url != nil {
			s.workers.Add(1)
			go s.scan(url)
		}
	}

	// Шаблоны для протоколов:
	// http://...
	// https://...
	// //...
	reg = regexp.MustCompile(`\/\/ *`)
	res = reg.FindAllIndex(body, -1)
	for i := 0; i < len(res); i++ {
		if url := s.searchLink(body, res[i][1]); url != nil {
			s.workers.Add(1)
			go s.scan(url)
		}
	}
}

func (this *Scanner) searchLink(b []byte, s int) *url.URL {
	// Когда нибудь я покрою тебя тестами..
	// Ищем кавычки, если ссылка в них обрамлена:
	var sep byte
	for i := s; i < len(b); i++ {
		r := b[i]
		if r == ' ' {
			continue
		}

		switch r {
		case '"', '\'', '`':
			sep = r
		}

		break
	}

	// Считываем ссылку:
	var str string
	if sep == 0 {
		// Ссылка вообще без кавычек!
		// Читаем до возможного разделителя:
		var ok bool
		var br1, br2, br3 int // Скобки: { ( <
		for i, c := s, false; i < len(b); i++ {
			r := b[i]
			if r == ' ' && c == false {
				continue
			}
			c = true

			switch r {
			case '{':
				br1++
			case '}':
				br1--
			case '(':
				br2++
			case ')':
				br2--
			case '<':
				br3++
			case '>':
				br3--
			}

			if (r <= ' ' ||
				r == '}' ||
				r == ')' ||
				r == '>') && (br1 <= 0 && br2 <= 0 && br3 <= 0) {
				str = string(b[s:i])
				ok = true
				break
			}
		}
		if !ok {
			str = string(b[s:])
		}
	} else {
		// Ссылка в кавычках:
		var ok bool
		for i := s + 1; i < len(b); i++ {
			if b[i] == sep && b[i-1] != '\'' {
				str = string(b[s+1 : i])
				ok = true
				break
			}
		}
		if !ok {
			str = string(b[s+1:])
		}
	}

	// Пытаемься распарсить в ссылку:
	u, e := url.Parse(str)
	if e != nil {
		log.Printf("Не удалось прочитать ссылку: %v", e.Error())
		return nil
	}

	// Относительные ссылки в абсолютные, чтоб программа могла
	// сравнить домен ссылки с родным: (Только внутри программы)
	if u.IsAbs() == false {
		u.Scheme = this.url.Scheme
		u.Host = this.url.Host
	}

	return u
}

// Получить каталог исполняемого файла
func (s *Scanner) binPath() (string, error) {
	path, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("Не удалось получить расположение исполняемого файла: %w", err)
	}
	return filepath.Dir(path), nil
}

// Проверка ссылки на интересующий нас протокол
func (s *Scanner) IsInterstingProtocol(url *url.URL) bool {

	// Список всех протоколов:
	// http://www.iana.org/assignments/uri-schemes/uri-schemes.xhtml

	switch url.Scheme {
	case "http", "https", "file":
		return true
	}

	return false
}

// Разбор пользовательского URL
func (s *Scanner) parseURL(text string) (*url.URL, error) {
	text = strings.ToLower(text)
	if len(text) < 5 {
		return nil, fmt.Errorf("Слишком короткий адрес сайта")
	}

	// HTTPS:
	if strings.Index(text, "https://") == 0 {
		return url.Parse(text)
	}

	// HTTP:
	if strings.Index(text, "http://") == 0 {
		return url.Parse(text)
	}

	// Относительный:
	if strings.Index(text, "//") == 0 {
		return url.Parse(text)
	}

	// Парсим:
	url, err := url.Parse("http://" + text)
	if err != nil {
		return nil, err
	}

	// Пустой URL:
	if url.Host == "" {
		return nil, fmt.Errorf("Не удалось однозначно прочитать URL")
	}

	return url, nil
}

// Статус сканирования
func (s *Scanner) State() ScannerState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Сканируемый URL, заданный пользователем
func (s *Scanner) Url() *url.URL {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.url
}

// Параметры сканера с которыми он был запущен.
func (s *Scanner) Params() ScannerParams {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.params
}

// Ошибка сканера.
// Дополнение к ошибочным состояниям сканера. Содержит более
// детальную информацию о причине сбоя для вывода пользователю.
//
// Примечания:
//   * По умолчанию равно nil;
//   * Всегда содержит последнюю возникшую ошибку, кроме ошибки
//     попытки запуска уже запущенного сканера. Эта ошибка
//     только возвращается методом Start();
//   * При повторном запуске сканнера сбрасывается на nil
//     (Корректный запуск без ошибок);
func (s *Scanner) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// Получить путь к корневой папке для данных сайта.
// Инициализируется каждый раз при вызова метода: Scanner.Start()
func (s *Scanner) Dir() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dir
}

// Дата запуска сканера.
func (s *Scanner) DateStart() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dateStart
}

// Дата первого запроса к сайту.
func (s *Scanner) DateScan() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dateScan
}

// Дата завершения сканирования.
func (s *Scanner) DateFinish() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.dateFinish
}

// Получить отчёт о текущем состоянии сканера.
func (s *Scanner) Report(full bool) string {
	const (
		sep  = " "
		len1 = 100
		len2 = 30
		len3 = 70
	)

	r := cell("URL", len1) + sep +
		cell("Тип", len2) + sep +
		cell("Статус", len3) + sep +
		"\n" + line(50) + "\n"

	var totalCount, totalCountExt, totalSize int64
	a := s.sources.List()
	for _, obj := range a {
		totalCount++

		obj.mu.RLock()
		if obj.isExternal {
			totalCountExt++
		} else {
			totalSize += obj.size
		}

		if !full && !(obj.state == SourceDownload || obj.state == SourceRead || obj.state == SourceRequest || obj.state == SourceSave) {
			obj.mu.RUnlock()
			continue
		}

		url := obj.url.String()
		mime := obj.mime
		status := s.repObjStatus(obj)
		obj.mu.RUnlock()

		r += cell(url, len1) + sep +
			cell(mime, len2) + sep +
			cell(status, len3) + sep +
			"\n"
	}

	s.mu.RLock()
	var threads = s.threads
	s.mu.RUnlock()

	return r + line(50) + "\n" +
		"\nКол-во горутин:           " + fmt.Sprint(threads) +
		"\nКол-во всех ссылок:       " + fmt.Sprint(totalCount) +
		"\nКол-во внешних ссылок:    " + fmt.Sprint(totalCountExt) +
		"\nКол-во внутренних ссылок: " + fmt.Sprint(totalCount-totalCountExt) +
		"\nОбъём данных:             " + s.repSize(float64(totalSize)) +
		"\nВремя работы:             " + s.repDuration(time.Since(s.DateStart()))
}

func line(l int) string {
	s := ""
	for i := 0; i < l; i++ {
		s = s + "-"
	}
	return s
}

// Получить содержимое ячейки
func cell(v string, lenMax int) string {
	runes := []rune(v)
	l := len(runes)

	// Ровно:
	if l == lenMax {
		return v
	}

	// Длинное:
	if l > lenMax {
		return "..." + string(runes[l-(lenMax-3):])
	}

	// Короткое:
	spaces := make([]rune, lenMax-l)
	for i := 0; i < len(spaces); i++ {
		spaces[i] = ' '
	}

	return v + string(spaces)
}

// Получить текстовое значение размера
func (s *Scanner) repSize(bytes float64) string {

	// Таблица измерения количества информации:
	// https://ru.wikipedia.org/wiki/%D0%9C%D0%B5%D0%B3%D0%B0%D0%B1%D0%B0%D0%B9%D1%82
	//
	// +------------------------------+
	// |        ГОСТ 8.417—2002       |
	// | Название Обозначение Степень |
	// +------------------------------+
	// | байт        Б         10^0   |
	// | килобайт    Кбайт     10^3   |
	// | мегабайт    Мбайт     10^6   |
	// | гигабайт    Гбайт     10^9   |
	// | терабайт    Тбайт     10^12  |
	// | петабайт    Пбайт     10^15  |
	// | эксабайт    Эбайт     10^18  |
	// | зеттабайт   Збайт     10^21  |
	// | йоттабайт   Ибайт     10^24  |
	// +------------------------------+

	if bytes < 1e3 {
		return fmt.Sprint(bytes) + " Б"
	}
	if bytes < 1e6 {
		return fmt.Sprint(math.Floor(bytes/1e1)/1e2) + " Кбайт"
	}
	if bytes < 1e9 {
		return fmt.Sprint(math.Floor(bytes/1e4)/1e2) + " Мбайт"
	}
	if bytes < 1e12 {
		return fmt.Sprint(math.Floor(bytes/1e7)/1e2) + " Гбайт"
	}
	if bytes < 1e15 {
		return fmt.Sprint(math.Floor(bytes/1e10)/1e2) + " Тбайт"
	}
	if bytes < 1e18 {
		return fmt.Sprint(math.Floor(bytes/1e13)/1e2) + " Пбайт"
	}
	if bytes < 1e21 {
		return fmt.Sprint(math.Floor(bytes/1e16)/1e2) + " Эбайт"
	}
	if bytes < 1e24 {
		return fmt.Sprint(math.Floor(bytes/1e19)/1e2) + " Збайт"
	}
	return fmt.Sprint(math.Floor(bytes/1e22)/1e2) + " Ибайт"
}

// Вывести прошедшее время
func (s *Scanner) repDuration(t time.Duration) string {
	h := math.Floor(t.Hours())
	m := math.Floor(t.Minutes())
	ss := math.Floor(t.Seconds())

	if h > 0 {
		return fmt.Sprintf("%v час. %v мин. %v сек.", h, m, ss)
	}
	if m > 0 {
		return fmt.Sprintf("%v мин. %v сек.", m, ss)
	}

	return fmt.Sprintf("%v сек.", ss)
}

// Получить текстовое значение размера
func (s *Scanner) repObjStatus(obj *Source) string {
	// п.с. Объект с RLock()
	switch obj.state {
	case SourceRequestWaitRepeat:
		return fmt.Sprintf("%v %v/%v", obj.state, obj.repeats, s.params.RepeatsMax)
	case SourceRequestError, SourceDownloadError, SourceSaveError:
		return fmt.Sprintf("Ошибка: %v: %v", obj.state, obj.err.Error())
	default:
		return obj.state.String()
	}
}
