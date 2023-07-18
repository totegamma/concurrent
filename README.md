# Concurrent
Concurrentは分散マイクロブログ基盤です。

## Motivation
Concurrentは、「セルフホストでお一人様インスタンス建てたい！けどローカルが1人ぼっちなのは寂しい...」という問題を解決するために生まれました。
個々のサーバーが所有しているタイムライン(mastodonやmisskeyで言うところのローカル)を別のサーバーから閲覧ないしは書き込みができます。
また、自分が閲覧しているタイムラインに対して、どのサーバーの持ち物であってもリアルタイムなイベントを得ることができます。

これにより、どのサーバーにいても世界は一つであるように、壁のないコミュニケーションが可能です。

## How it works
Concurrentでは公開鍵を用いて、エンティティ(ユーザー)が発行するメッセージ(Twitterで言うところのツイート)に署名を行います。

これにより、そのツイートがその秘密鍵の持ち主によって行われたことが誰でも検証できるようになります。

ConcurrentではユーザーのIDはConcurrentアドレス(cc-address)(例えば、`CC3E31b2957F984e378EC5cE84AaC3871147BD7bBF`)を用いて識別されます。

## Getting Started
### インスタンスを立ち上げる

#### with docker compose
このレポジトリ内のcomposeファイルがそのまま使えます。

`config/config.yaml`を編集します。

concurrentのインスタンス用のCCID, privatekey, publickeyは、concurrent.worldのdevツールを使うと便利に生成できます。

#### with k8s
helmchartがあります: https://helmcharts.gammalab.net
チャート本体のレポジトリ: https://github.com/totegamma/helmcharts/tree/master/charts/concurrent

valuesに入れる値はdocker composeを使う場合のconfigの生成方法を参考にしてください。

モニタリングを有効にする場合はValues.observabilityにgrafanacloudの各種認証情報を追加すると利用できます。

### インスタンスを連合する
インスタンスを立ち上げたら、好きなほかのサーバーと連合しましょう。
連合は、インスタンスの管理画面から行うことができます。

管理画面はブラウザから `<host>/login?token=<jwt>` にアクセスすることで可能です。ここで使うjwtは、config.yamlでadminとして指定したユーザーである必要があります。
この画面はconcurrent.worldの設定タブにある"go to domain home"ボタンを使うと簡単にアクセスすることができます。

まずは、hub.concurrent.worldと連合してみましょう。
hostタブで`hub.concurrent.world`と入力し、goボタンを押すだけで連合が完了します。

## Contributing
コードのPRは必ずissueでその可否のコンセンサスをとってからにしてください(せっかく作ってくれたPRをcloseするのは心が痛いので)。

