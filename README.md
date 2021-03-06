### Получить копию!
В целях практики работы с go и многопоточным программированием написал консольную приблуду для скачивания любого сайта из сети на локальную машину. (Создаётся статичная копия сайта)
Программа анализирует заданный URL и пытается найти любые другие ссылки, затем пишет полученный файл на диск. Для каждой найденной ссылки рекурсивно запускается отдельный поток (горутина) для последующего анализа и поиска новых ссылок и так до тех пор, пока весь сайт к хуям не будет скачан со всеми потрохами. Обрабатываются только те ссылки, которые расположены на исходном домене указанного URL. Ссылки на сторонние домены пропускаются, чтоб случайно не скачать весь остальной интернет.

## Некоторые моменты:

1. Все горутины синхронизированы между собой таким образом, чтобы не обрабатывать одинаковые URL или не превысить кол-во одновременных, сетевых запросов. (Горутины встают в очередь при достижении лимита в 20 параллельных запросов);
2. Главный поток после запуска сканирования считывает состояние программы 2 раза в секунду и пишет на экране текущие, обрабатываемые URL, ждёт завершения сканирования;
3. При запросе каждого URL программа определяет полученный тип данных на основе первых байт (Magic bytes), чтобы применить правильный анализ;
4. При получений от сервера 503 кода (Превышение лимита запросов), немного ждёт и пытается снова сделать запрос до тех пор, пока не получит любой другой ответ сервера;
5. Каждый найденный URL обрабатывается только 1 раз;
6. HTML файлы анализируются полноценно, как DOM модели. Выдираются ссылки из таких тегов, как: <a>, <script>, <link>, <img> ...;
7. Текстовые файлы, такие как: CSS, JavaScript - анализируются простым поиском ссылок по шаблону;

Программа довольно простая и может не учитывать множество нюансов специфики работы сети интернет, коих дохера. Но для протестированных мною сайтов успешно выкачала страницы и их контент, сохранив затем на диск за довольно короткое время.

## Личные ощущения
Работать с параллельностью в go очень легко и приятно, если изначально качественно подойти к проектированию. Если спроектировать плохо, то, чувствую, будет боль, ад и анархия :) Базовые инструменты простые, а запуск нового потока выполняется всего в 2 символа: "go". Но эта простота убьёт вас, если вы будете необдуманно запускать параллельно всё подряд.

Есть все необходимые стандартные библиотеки для работы с сетью или файловой системой. При этом вам не нужно переживать из-за кроссплатформенности, за вас уже подумали о том, что, например, разделитель пути в Windows и Linux различаются, как и о многих других вещах. Go кроссплатформенный, несмотря на отсутствие VM. За это ему отдельный, огромный плюс. Virtual Machine для go - не нужен, а вся рабочая программа в собранном виде весит копейки - меньше 10 Мб. Привет Java, C# :) 

Дебагинг пока осуществлял только простой. Есть стандартный stack trace с указанием файла и номера строки при возникновении паники в go. Знаю, что есть и более расширенные инструменты для отладки, например функция компилятора для поиска состояния гонки. Пока этим не пользовался, не доводил до такого :)

Порадовали и новшества для меня, которые даёт go. Возврат более 1 значения из функций, более простая обработка ошибок, возможность использования меток (Да, иногда они действительно полезны и упрощают программу, если не злоупотреблять). Автоматическое форматирование текста, чтоб у всех код выглядел одинаково (На самом деле процентов на 90%, есть ещё 10% места, где вы таки можете применить свой уникальный, ебучий авторский стиль форматирования исходного кода :) ) Дженерики тоже есть, что удивило. Вроде как их завезли недавно, хотя мне они нужны не то чтобы часто.

Порадовало мнение разрабов go по многим вопросам дизайна. Например, по поводу необходимости обязательного комментирования кода, или рекомендация по способу построения тела функции, в которой в случае ошибки функция завершается досрочно вызовом return, а основное её тело должно быть предназначено для ожидаемого, корректного выполнения. Таким образом тело функции получается плоским и простым, без этих ваших адских вложенностей из кучи {{{{{ .!. }}}}}. Порадовало, потому что я встречал противоположные мнения, а некоторые даже пытались убедить, что так правильно и именно так и надо делать.

В целом, разработка на go напрямую зависит от ровности рук, как наверное и везде. Но go в некоторых местах пытается выпрямить вам руки насильно, моё уважение ))