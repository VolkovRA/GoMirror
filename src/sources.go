package main

import (
	"net/url"
	"sync"
)

// Статус ресурса.
type SourceState int

// Получить текстовое представление статуса ресурса.
func (v SourceState) String() string {
	switch v {
	case SourceWait:
		return "Ожидание"
	case SourceRequest:
		return "Запрос"
	case SourceRequestWaitRepeat:
		return "Ожидание повторной попытки запроса"
	case SourceRequestError:
		return "Ошибка запроса"
	case SourceDownload:
		return "Скачивание"
	case SourceDownloadError:
		return "Ошибка скачивания"
	case SourceRead:
		return "Чтение"
	case SourceSave:
		return "Сохранение"
	case SourceSaveError:
		return "Ошибка сохранения"
	case SourceComplete:
		return "Сохранён"
	case SourceSkip:
		return "Пропуск"
	default:
		return "Unknown"
	}
}

const (

	// Ожидание очереди на скачивание ресурса
	SourceWait SourceState = iota

	// Выполняется запроc ресурса
	SourceRequest

	// Ожидание следующей попытки запроса
	SourceRequestWaitRepeat

	// Ошибка запроса ресурса
	SourceRequestError

	// Скачивание тела ресурса
	SourceDownload

	// Ошибка скачивания тела ресурса
	SourceDownloadError

	// Чтение/парсинг ресурса
	SourceRead

	// Сохранение ресурса в файловую систему
	SourceSave

	// Ошибка сохранения ресурса в файловую систему
	SourceSaveError

	// Ресурс сохранён в файловую систему
	SourceComplete

	// Пропуск ресурса
	SourceSkip
)

// Ресурс на сайте
type Source struct {
	mu            sync.RWMutex
	url           *url.URL    // URL Для запроса ресурса
	state         SourceState // Текущий статус обработки ресурса
	mime          string      // Mime тип ресурса: http.DetectContentType()
	size          int64       // Размер в байтах
	isExternal    bool        // Флаг внешнего ресурса. Внешние ресурсы не запрашиваются и только для статистики
	isInteresting bool        // Флаг интересного ресурса. См.: Scanner.IsInterstingProtocol()
	err           error       // Ошибка основной обработки ресурса
	errRead       error       // Ошибка анализа ресурса (Второстепенная, не блокирующая)
	repeats       int         // Счётчик повторных попыток запроса из-за ошибок
}

// URL Адрес ресурса.
//
// Значение доступно сразу после создания ресурса и
// оно не меняется. Не может быть nil.
func (s *Source) URL() *url.URL {
	return s.url
}

// Флаг внешнего ресурса.
// Внешние ресурсы не запрашиваются и используются
// в программе только для статистики.
func (s *Source) IsExternal() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isExternal
}

// Флаг интересного ресурса.
// См.: Scanner.IsInterstingProtocol()
func (s *Source) IsInteresting() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.isInteresting
}

// Статус ресурса.
// Изменяется по ходу обработки ресурса программой.
func (s *Source) State() SourceState {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.state
}

// Mime тип ресурса: http.DetectContentType()
// Становится доступно только после скачивания
// ресурса и не для внешних ресурсов.
func (s *Source) Mime() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.mime
}

// Размер в байтах.
// Становится доступно только после скачивания
// ресурса и не для внешних ресурсов.
func (s *Source) Size() int64 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.size
}

// Ошибка обработки ресурса.
// Используется как дополнение для состояний ресурса,
// указывающих на ошибку обработки.
func (s *Source) Err() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.err
}

// Ошибка анализа ресурса.
// Означает об ошибках поиска доп. ссылок в ресурсе,
// не блокирует обработку самого ресурса.
func (s *Source) ErrRead() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.errRead
}

// Список ресурсов
type Sources struct {
	mu sync.RWMutex
	m  map[string]*Source
	a  []*Source
	p  *Scanner
}

// Создать новый список
func newSources(parent *Scanner) *Sources {
	return &Sources{
		p: parent,
		m: make(map[string]*Source),
		a: make([]*Source, 0, 100),
	}
}

// Добавить ресурс.
//   * Если список уже содержит элемент с таким URL,
//     возвращает его, а не создаёт новый;
//   * Если в списке нет ресурса с таким URL, то создаёт
//     и возвращает новый ресурс.
//
// Метод всегда возвращает экземпляр, который не может быть nil.
func (s *Sources) Add(url *url.URL) (*Source, bool) {
	key := url.String()

	s.mu.Lock()
	defer s.mu.Unlock()

	// Поиск:
	v, ok := s.m[key]
	if ok {
		return v, false
	}

	// Создаём новый:
	obj := &Source{
		url:           url,
		isExternal:    s.p.url.Hostname() != url.Hostname(),
		isInteresting: s.p.IsInterstingProtocol(url),
	}
	s.a = append(s.a, obj)
	s.m[key] = obj

	return obj, true
}

// Получить копию среза всех элементов.
// Полезно для обхода циклом. Полученный список безопасен
// для внесения изменений.
func (s *Sources) List() []*Source {
	s.mu.RLock()
	defer s.mu.RUnlock()

	a2 := make([]*Source, len(s.a))
	copy(a2, s.a)

	return a2
}
