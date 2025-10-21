package main

// compile command go build -ldflags="-H=windowsgui" -o chatlogTranslator.exe

import (
    "bufio"
    "fmt"
    "io/ioutil"
    "net/http"
    "net/url"
    "strings"
    "regexp"
    "os"
    "encoding/json"
    "sort"
    "time"
    "path/filepath"
    "github.com/getlantern/systray"
    "github.com/yuin/gopher-lua"
    "golang.org/x/sys/windows"
    "unsafe"
)

const (
    addonFolder = "./addons/ChatLogTranslator/"
    logFolder = addonFolder + "log/"
    reLogName = `^chat\d+\.txt$`
    reApiKey = `deeplApiKey = (.+)$`
    translatedChatlogName = "translatedChat"
    infoLogFile = logFolder + "translation_info.txt"
    errorLogFile = logFolder + "translation_error.txt"
    endpoint = "https://api-free.deepl.com/v2/translate"
    iconFile = addonFolder + "redria.ico"
    luaOptions = addonFolder + "options.lua"
    gameWindowTitle = "Ephinea: Phantasy Star Online Blue Burst"
)

var languages = []string{
    "AR",
    "BG",
    "CS",
    "DA",
    "DE",
    "EL",
    "EN",
    "EN-GB",
    "EN-US",
    "ES",
    "ES-419",
    "ET",
    "FI",
    "FR",
    "HE",
    "HU",
    "ID",
    "IT",
    "JA",
    "KO",
    "LT",
    "LV",
    "NB",
    "NL",
    "PL",
    "PT",
    "PT-BR",
    "PT-PT",
    "RO",
    "RU",
    "SK",
    "SL",
    "SV",
    "TH",
    "TR",
    "UK",
    "VI",
    "ZH",
    "ZH-HANS",
    "ZH-HANT",
}

var mutex windows.Handle

type Translation struct {
    Text string `json:"text"`
}

type DeepLResponse struct {
    Translations []Translation `json:"translations"`
}

func write (path string, message string) {
    os.WriteFile(path, []byte(message + "\n"), 0644)
}

// append log function
// func write(path string, message string) {
//     ft, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
//     if err != nil {
//         fmt.Println("Failed to open logger file:", err)
//     }

//     _, err = ft.WriteString(message + "\n")
//     if err != nil {
//         fmt.Println("Failed to write to logger file:", err)
//     }
// }

func infoLog(args ...interface{}) {
    // timestamp := time.Now().Format("2006-01-02 15:04:05")
    // msg := fmt.Sprint(args...)
    // line := timestamp + "\t" + msg

    // write(infoLogFile, line)
    // fmt.Println(line)
}

func errorLog(args ...interface{}) {
    timestamp := time.Now().Format("2006-01-02 15:04:05")
    msg := fmt.Sprint(args...)
    line := timestamp + "\t" + msg

    write(errorLogFile, line)
    // fmt.Println(line)
}

func loadIcon() []byte {
    data, err := ioutil.ReadFile(iconFile)
    if err != nil {
        errorLog("Warning: failed to load icon. ", err)
        return nil
    } else {
        return data
    }
}

func findWindow(title string) (uintptr, error) {
    user32 := windows.NewLazySystemDLL("user32.dll")
    procFindWindow := user32.NewProc("FindWindowW")

    ptr := windows.StringToUTF16Ptr(title)
    hwnd, _, err := procFindWindow.Call(
        0, // class name (null)
        uintptr(unsafe.Pointer(ptr)),
    )
	if hwnd == 0 {
		return 0, err
	}
    return hwnd, nil
}


func messageBox(title, message string) {
    windows.MessageBox(0,
        windows.StringToUTF16Ptr(message),
        windows.StringToUTF16Ptr(title),
        windows.MB_OK|windows.MB_ICONWARNING)
}

func main() {

    // If PSOBB is not running, terminate the process.
    _, err := findWindow(gameWindowTitle)
	if err != nil {
        messageBox("Warning", "PSOBB is not running. Start the PSOBB first.")
        return
	}

    name, _ := windows.UTF16PtrFromString("Global\\TranlatorChatLogMutex")
    mutex, err := windows.CreateMutex(nil, false, name)
    if err != nil {
        // errorLog("Failed to create mutex. ", err)
        return
    }

    lastErr := windows.GetLastError()
    if lastErr == windows.ERROR_ALREADY_EXISTS {
        // errorLog("Another instance is already running. Exiting.")
        return
    }

    defer windows.ReleaseMutex(mutex)
    defer windows.CloseHandle(mutex)

    systray.Run(onReady, onExit)
}

func onReady() {
    infoLog("start appication...")

    systray.SetIcon(loadIcon())
    systray.SetTitle("Ephinea ChatLogTranslator")
    systray.SetTooltip("Ephinea ChatLogTranslator")

    mQuit := systray.AddMenuItem("Quit", "Quit the application")

    go func() {
        for {
            select {
            case <-mQuit.ClickedCh:
                infoLog("Exiting...")
                systray.Quit()
                return
            }
        }

    }()

    go func() {
        for {
            // If PSOBB is stopped, terminate the process.
            _, err := findWindow(gameWindowTitle)
                if err != nil {
                    os.Exit(0)
            }

            infoLog("execute...")

            checkAndTranslateFiles()
            // sleep for next roop
            time.Sleep(5 * time.Second)
        }
    }()
    
}

func onExit() {}

func isSafeFileName(name string) bool {
    if name == "" {
        errorLog("File name is empty. Cannot delete. " + name)
        return false
    }
    if name == "." || name == ".." {
        errorLog("File name is only dot. Cannot delete. " + name)
        return false
    }
    if name == "/" || name == "\\" {
        errorLog("File name is only a slash or backslash. Cannot delete. " + name)
        return false
    }
    if strings.ContainsAny(name, `<>:"|?*`) {
        errorLog("File name contains invalid characters. Cannot delete. " + name)
        return false
    }
    // try to matching chat log regex
    base := filepath.Base(name)
    fileReg := regexp.MustCompile(reLogName)
    if !fileReg.MatchString(base) {
        errorLog("File name does not match the chat log pattern. Cannot delete. " + name)
        return false
    }
    return true
}

func translateByDeeplApi(messages [][]string, apiKey string, language string, dateTranslatedChatlogFile string) {
    data := url.Values{}
    data.Set("auth_key", apiKey)
    data.Set("target_lang", language)

    var respStruct DeepLResponse
    for _, message := range messages {
        data.Add("text", message[2])
    }

    // GETリクエスト
    resp, err := http.Post(endpoint, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
    if err != nil {
        errorLog("translation request error.", err)
        return
    }

    if resp.StatusCode != 200 {
        errorLog("translation request error. please check your DeepL API Key.")
        return
    }

    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        errorLog("Failed to read response body.", err.Error())
        return
    }

    err = json.Unmarshal(body, &respStruct)
    if err != nil {
        errorLog("failed to parse JSON response.", err)
        return
    }

    f, err := os.OpenFile(dateTranslatedChatlogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        errorLog("Failed to open translated log file:", err)
        return
    }
    defer f.Close()

    for i, t := range respStruct.Translations {
        line := messages[i][0] + "\t" + messages[i][1] + "\t" + messages[i][2] + "\t" + t.Text + "\n"

        if _, err := f.WriteString(line); err != nil {
            errorLog("Failed to write to translated log file:", err)
            return
        }
    }
    resp.Body.Close()
}

func checkAndTranslateFiles() {
    infoLog("translator start...")

    // get apiKey
    var apiKey string
    var language string

    L := lua.NewState()
    defer L.Close()

    if err := L.DoFile(luaOptions); err != nil {
        errorLog("DeepL API key not set. Please set it in the configuration.")
        return
    }

    tbl := L.Get(-1)
    if tblTable, ok := tbl.(*lua.LTable); ok {
        apiKeyVal := tblTable.RawGetString("deeplApiKey")
        apiKeyStr, ok := apiKeyVal.(lua.LString)
        if !ok {
            errorLog("DeepL API key not set. Please set it in the configuration.")
            return
        }
        if string(apiKeyStr) == "" {
            errorLog("DeepL API Key not set. Please set it.")
            return
        }
        apiKey = string(apiKeyStr)

        languageVal  := tblTable.RawGetString("language")
        num, ok := languageVal.(lua.LNumber)
        if !ok {
            errorLog("Language value is not a number.")
            return
        }

        index := int(num) -1
        if index >= 0 && index < len(languages) {
            language = languages[index]
        } else {
            language = "EN"
        }
    }

    // log file check
    files, err := os.ReadDir(logFolder)
    if err != nil {
        errorLog("could not find log folder.")
        return
    }

    fileReg := regexp.MustCompile(reLogName)
    var filenames []string

    for _, f := range files {
        if f.IsDir() {
            continue
        }
        if fileReg.MatchString(f.Name()) {
            filenames = append(filenames, f.Name())
        }
    }

    sort.Strings(filenames)

    var messages [][]string
    for _, filename := range filenames {
        logFile, err := os.Open(logFolder + filename)
        if err != nil {
            errorLog("Failed to open chatlog file.", err)
            break
        }

        scanner := bufio.NewScanner(logFile)
        for scanner.Scan() {
            
            line := scanner.Text()
            parts := strings.Split(line, "\t")
            if len(parts) >= 3 {
                messages = append(messages, parts)
            }
        }
        
        logFile.Close()
    }

    // today Date
    dateStr := time.Now().Format("20060102")
	dateTranslatedChatlogFile := fmt.Sprintf("%s%s%s.txt", logFolder, translatedChatlogName, dateStr)

    if len(messages) > 0 {
       translateByDeeplApi(messages, apiKey, language, dateTranslatedChatlogFile)
    }

    // delete translated log files
    for _, filename := range filenames {
        path := logFolder + filename
        if !isSafeFileName(path) {
            errorLog("Failed to delete translated log file. file name is not safety.")
            break 
        }
        err := os.Remove(path)
        if err != nil {
            errorLog("Failed to delete translated log file.")
        }
    }

    // delete error file
    if _, err := os.Stat(errorLogFile); err == nil {
        err = os.Remove(errorLogFile)
        if err != nil {
            errorLog("Failed to delete translated error log file:", err)
        }
    }
}
