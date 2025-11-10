# psobb-chatlog-with-translator

Added a translation feature to the Ephinea PSOBB ChatLog addon.<br>

The external background application translates the chat log, and the addon reads it to display the translated messages in the in-game UI.<br>
Please do not delete the log folder.<br>
If it has been deleted, please create a new log folder or download the addon again.<br>

Ephinea PSOBBのチャットログアドオンに翻訳機能を追加しました。<br>
常駐外部アプリで翻訳したチャットログをアドオンで読み取り、ゲーム内でUIに表示します。<br>
logフォルダは削除しないでください。削除した場合、logフォルダを作成するか、新しくアドオンをダウンロードしてください。<br>

▪️Forum<br>
https://www.pioneer2.net/community/threads/chatlog-addon-with-translation.31888/

▪️How to create a Google App Script<br>
https://youtu.be/qSxsuHmwRvc
```
function doPost(e) {
  const data = JSON.parse(e.postData.contents);
  const texts = data.texts;
  const target = data.target;

  const results = texts.map(t => LanguageApp.translate(t, '', target));
  return ContentService.createTextOutput(JSON.stringify(results));
}
```
