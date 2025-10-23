package main

// compile command go build -ldflags="-H=windowsgui" -o chatlogTranslator.exe

import (
    "bytes"
    "bufio"
    "fmt"
    "io"
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
    enableAppendLog = true
    enableInfoLog = true
    addonFolder = "./addons/ChatLogTranslator/"
    logFolder = addonFolder + "log/"
    reLogName = `^chat\d+\.txt$`
    translatedChatlogName = "translatedChat"
    infoLogFile = logFolder + "translation_info.txt"
    errorLogFile = logFolder + "translation_error.txt"
    deeplUrl = "https://api-free.deepl.com/v2/translate"
    gasUrl = "https://script.google.com/macros/s/"
    iconFile = addonFolder + "redria.ico"
    luaOptions = addonFolder + "options.lua"
    gameWindowTitle = "Ephinea: Phantasy Star Online Blue Burst"
    deeplMode = 1
    googleMode = 2
)

var deeplLanguages = []string{
    "EN-US",
    "EN-GB",
    "JA",
    "KO",
    "ZH-HANS",
    "ZH-HANT",
    "FR",
    "DE",
    "ES",
    "PT-BR",
    "RU",
    "IT",
    "TH",
    "VI",
    "ID",
    "AR",
}

var googleLanguages = []string{
    "en",
    "en",
    "ja",
    "ko",
    "zh-CN",
    "zh-TW",
    "fr",
    "de",
    "es",
    "pt-BR",
    "ru",
    "it",
    "th",
    "vi",
    "id",
    "ar",
}

// append log function
func write(path string, message string) {
    if enableAppendLog {
        ft, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
        if err != nil {
            fmt.Println("Failed to open logger file:", err)
        }

        _, err = ft.WriteString(message + "\n")
        if err != nil {
            fmt.Println("Failed to write to logger file:", err)
        }
    } else {
        os.WriteFile(path, []byte(message + "\n"), 0644)
    }
}

func infoLog(args ...interface{}) {
    if enableInfoLog {
        timestamp := time.Now().Format("2006-01-02 15:04:05")
        msg := fmt.Sprint(args...)
        line := timestamp + "\t" + msg

        write(infoLogFile, line)
        // fmt.Println(line)
    }
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

func getChatlogFilenames() []string {
    files, err := os.ReadDir(logFolder)
    if err != nil {
        errorLog("could not find log folder.")
        return nil
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

    return filenames
}

func readChatlogMessages(filenames []string) [][]string {
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

    return messages
}

func writeTranslatedLog(originalChat [][]string, transResults []string) {

    // today Date
    dateStr := time.Now().Format("20060102")
	dateLog := fmt.Sprintf("%s%s%s.txt", logFolder, translatedChatlogName, dateStr)

    f, err := os.OpenFile(dateLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
    if err != nil {
        errorLog("Failed to open translated log file.")
        return
    }
    defer f.Close()

    for i, t := range transResults {
        line := originalChat[i][0] + "\t" + originalChat[i][1] + "\t" + originalChat[i][2] + "\t" + t + "\n"

        if _, err := f.WriteString(line); err != nil {
            errorLog("Failed to write to translated log file.")
            return
        }
    }
}

func translateByDeeplApi(messages [][]string, apiKey string, language string) {
    
    type DeepLResponse struct {
        Translations []struct {
            Text string `json:"text"`
        } `json:"translations"`
    }

    data := url.Values{}
    data.Set("auth_key", apiKey)
    data.Set("target_lang", language)

    var results DeepLResponse
    for _, message := range messages {
        data.Add("text", message[2])
    }

    // http request
    resp, err := http.Post(deeplUrl, "application/x-www-form-urlencoded", strings.NewReader(data.Encode()))
    if err != nil {
        errorLog("translation request error. (Deepl)")
        return
    }
    defer resp.Body.Close() 

    // check status
    if resp.StatusCode != http.StatusOK {
        errorLog("translation request error. please check your DeepL API Key.")
        return
    }

    // check read body
    body, err := ioutil.ReadAll(resp.Body)
    if err != nil {
        errorLog("Failed to read response body. (Deepl)")
        return
    }

    // check parse
    err = json.Unmarshal(body, &results)
    if err != nil {
        errorLog("failed to parse JSON response. (Deepl)")
        return
    }

    // convert to array
    _results := make([]string, len(results.Translations))
     for i, t := range results.Translations {
        _results[i] = t.Text
    }

    // write log
    writeTranslatedLog(messages, _results)
}

func translateByGas(messages [][]string, depId string, language string) {
    apiURL := gasUrl + depId + "/exec"

	texts := []string{}
    for _, message := range messages {
        texts = append(texts, message[2])
    }

    jsonData := map[string]interface{}{
        "texts":  texts,
        "target": language,
    }

    b, _ := json.Marshal(jsonData)
    resp, err := http.Post(apiURL, "application/json", bytes.NewBuffer(b))
	if err != nil {
        errorLog("translation request error. (Gas)")
		return
	}
	defer resp.Body.Close()

	// check status
	if resp.StatusCode != http.StatusOK {
        errorLog("translation request error. please check your Google App Script Deplopment ID.")
		return
	}

    // check read body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
        errorLog("Failed to read response body. (Gas)")
		return
	}

    // check parse
    var results []string
    if err := json.Unmarshal(body, &results); err != nil {
        errorLog("failed to parse JSON response. (Gas)")
        return
    }

    // write log
    writeTranslatedLog(messages, results)
}

func checkAndTranslateFiles() {
    infoLog("translator start...")

    // get apiKey
    var apiKey string
    var depId string
    var language string
    var transMode int

    L := lua.NewState()
    defer L.Close()

    if err := L.DoFile(luaOptions); err != nil {
        errorLog("DeepL API key not set. Please set it in the configuration.")
        return
    }

    tbl := L.Get(-1)
    if tblTable, ok := tbl.(*lua.LTable); ok {

        langVal  := tblTable.RawGetString("language")
        langFloat, ok := langVal.(lua.LNumber)
        if !ok {
            errorLog("Language value is not a number.")
            return
        }

        langIndex := int(langFloat) -1

        transModeVal  := tblTable.RawGetString("translationMode")
        tFloat, ok := transModeVal.(lua.LNumber)
        if !ok {
            errorLog("translation mode value is not a number.")
            return
        }
    
        transMode = int(tFloat)

        if (transMode == deeplMode) {
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
            
            if langIndex >= 0 && langIndex < len(deeplLanguages) {
                language = deeplLanguages[langIndex]
            } else {
                language = "EN-US"
            }
        }

        if (transMode == googleMode) {
            depIdyVal := tblTable.RawGetString("googleAppScriptDeploymentId")
            depIdyStr, ok := depIdyVal.(lua.LString)
            if !ok {
                errorLog("Google App Script Deployment ID not set. Please set it in the configuration.")
                return
            }
            if string(depIdyStr) == "" {
                errorLog("Google App Script Deployment ID not set. Please set it.")
                return
            }
            depId = string(depIdyStr)

            if langIndex >= 0 && langIndex < len(googleLanguages) {
                language = googleLanguages[langIndex]
            } else {
                language = "en"
            }
        }

    }

    // log file check
    filenames := getChatlogFilenames()
    if filenames == nil {
        return
    }
    messages := readChatlogMessages(filenames)

    if len(messages) > 0 {

        if transMode == deeplMode {
            translateByDeeplApi(messages, apiKey, language)
        }
        if transMode == googleMode {
            translateByGas(messages, depId, language)
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
                errorLog("Failed to delete translated error log file:")
            }
        }
    }
}
