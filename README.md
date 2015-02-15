# netclip
netclip はネットワーク越しにクリップボードにデータを送信するソフトウェアです。

# 用途
リモートに SSH でログインして作業しているとき、リモートでコピーしてローカルでペーストしたいことがよくあります。
ターミナルエミュレータ上の出力をマウスで範囲選択しコピーしてる方も多いのではないでしょうか。

もしリモート先で screen 系コマンドを使っていたりすると、マウスの選択は上手くいかないことがあります。
単語の選択ならまだしも、複数行選択だと末尾にたくさんの空白文字が残ってしまいませんか？

netclip を使うと、リモート先の任意の文字列をローカルのクリップボードに転送できます。
SSH に対応していますので、 SSH ログインが出来る環境なら使うことができます。

# 使い方
リモート側とローカル側での準備が必要です。

## リモート側の準備
netclip コマンドをインストールし、任意の場所に fifo ファイルを生成します。
リモート側は Mac OSX Mavericks で動作確認を取っていますが、
Go 言語が動く POSIX なシステムなら同じように動かせると思います。
リモート側は Windows には対応していません（fifo ファイルを作れないため）。

- コマンドのインストール  

  ```
  $ cd $HOME
  $ mkdir go
  $ cd go
  $ export GOPATH=`pwd`
  $ mkdir -p src/github.com/uchan-nos
  $ cd src/github.com/uchan-nos
  $ git clone https://github.com/uchan-nos/netclip.git
  $ cd netclip
  $ go get
  $ go build
  ```
  
  要するに普通の Go 言語プログラムですから Go 言語の習慣に従ってビルドするだけです。
  もし可能なら、生成された netclip コマンドを /usr/bin など、
  SSH 環境下でも標準でパスが通るような場所にコピーしておくと楽です。
  （/usr/local/bin は SSH 環境下だとパスが通っていない場合がありますので注意が必要です。）

- fifo ファイルの生成  

  ```
  $ cd $HOME
  $ mkfifo clip
  $ chmod 600 clip
  ```
  
  clip という名前付きパイプを通して文字列をローカル側に転送します。
  書き込み権を自分だけに制限することで、他人が勝手にクリップボードに書き込みするようなイタズラができなくなります。
  
## ローカル側の準備
リモート側と同様に netclip コマンドをビルドします。
ローカル側は Windows 7 で動作確認を取っていますが、
それ以外の Windows や Go 言語が動く POSIX なシステムでも同じように動かせると思います。

  ```
  > cd \Users\your-name
  > mkdir go
  > cd go
  > set GOPATH=%CD%
  > mkdir src\github.com\uchan-nos
  > cd src\github.com\uchan-nos
  > git clone https://github.com/uchan-nos/netclip.git
  > cd netclip
  > go get
  > go build
  ```

## サーバの起動
リモート側、ローカル側ともに netclip コマンドがビルドできれば、後はローカル側でサーバを立てるだけです。

```
> netclip.exe server -netclip /Users/your-name/clip -use-passwd 192.168.0.2:22
```

これでローカル側でサーバが立ち上がりますので、リモート側で clip ファイルに対して書き込みしてみます。

```
$ echo "hogehoge" > ~/clip
```

コマンドを実行したら、ローカル側で Ctrl+V してください。 hogehoge が貼り付けられるはずです。

## コマンドオプション
netclip server コマンドのオプションを説明します。

| オプション | 初期値 | 説明 |
|------------|--------|------|
| -help | | ヘルプを表示します |
| -key <path> | C:\Users\your-name\.ssh\id_rsa | SSH 通信で使うための秘密鍵へのパスです |
| -use-passwd | SSH の認証で公開鍵認証の代わりにパスワード認証を使います |
| -user <name> | SSH の認証で使うユーザ名です。リモートにログインするためのユーザ名を指定します |
| -clip <path> | $HOME/clip | リモート側での clip ファイルへのパスです |
| -netclip <path> | リモート側での netclip コマンドへのパスです。 netclip コマンドにパスが通ってない場合に指定します |

`-key` や `-user` の初期値は、コマンド実行時の環境変数から自動推定するため、
上記の初期値とは異なる場合があります。
`-help` コマンドで初期値を確認してください。

### SSH 実行時の $PATH に注意
netclip コマンドにパスが通っていない場合、

```
2015/02/15 14:33:31 executing remote netclip command.
2015/02/15 14:33:31 EOF
```

のようなログが出続けます。
SSH コマンドでは環境変数がリセットされますので、
.bashrc などで設定した値は読み込まれません。

netclip コマンドを /usr/local/bin などにコピーしておいても
実はパスが通ってなかった、なんてことがよくあります。
そんなときは `-netclip` オプションでコマンドへのパスを明示してください。