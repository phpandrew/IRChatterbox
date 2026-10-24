# soju with encrypted history (IRChatterbox build)

This is [soju](https://codeberg.org/emersion/soju) v0.11.1 plus one patch,
`irchatterbox-soju-encryption.patch`. It lets each bouncer user give soju an
[age](https://age-encryption.org) public key. soju then stores the text of that user's messages
encrypted, so the database on the server can't be read without the user's private key. The private
key stays in IRChatterbox (server settings → Bouncer → History decryption key).

## What changes

- **Stored text is encrypted.** That covers the text of PRIVMSG and NOTICE lines (including /me).
  Who sent each message, where and when stay readable, because soju needs them to file and replay
  history.
- **Setting a key also encrypts existing history**, in the background. Afterwards the search index
  is rebuilt and the database is checkpointed, and `secure_delete` is on, so old text doesn't stay
  in free pages or the WAL.
- **Live messages are unchanged.** Connected clients get them as normal. Only replayed history
  (backlog, CHATHISTORY) arrives encrypted: the ciphertext is in the `irchatterbox.com/enc` tag and
  the text is a placeholder.
- **New capability `irchatterbox.com/enc`.** soju drops custom tags on networks without
  `message-tags` (EFnet, IRCnet, Undernet, Rizon, ngircd servers). A client that asks for this cap
  gets the ciphertext on every network. IRChatterbox asks for it.
- **Other clients** (or IRChatterbox without the private key) see
  `[encrypted message: add your private key in IRChatterbox to read it]`.
- **Server-side search** (`soju.im/search`) can't find encrypted messages.
- **Requires** `message-store db` with SQLite. Postgres can store keys, but encrypting existing
  history isn't supported there.

Keys are set per user, and a network can have its own key that takes priority (handy for trying
encryption on one network first):

    /msg BouncerServ user update -encryption-key age1...             (your whole account)
    /msg BouncerServ user update sam -encryption-key age1...         (admin, someone else's)
    /msg BouncerServ user update -encryption-key ""                  (remove)
    /msg BouncerServ network update libera -encryption-key age1...   (one network only)
    /msg BouncerServ network update libera -encryption-key ""        (back to the account key)

Setting a key encrypts the stored history it covers, in the background: a network key covers that
network; the account key covers every network without its own key. Changing only a network's key
doesn't reconnect it. `network status` shows `encrypted (network key)` or `encrypted (account key)`.
In IRChatterbox, put the matching private key in that server's settings (each bouncer network is
its own server there).

On the hosted IRChatterbox bouncer, the account page does this for you.

Changing the key leaves older history encrypted with the old key. Keep the old private key on its
own line in IRChatterbox and both keep working. Removing the key stores new history readable;
what's already encrypted stays encrypted.

## Build

    ./build.sh          needs git and Go 1.22+; writes out/soju and out/sojuctl

## Deploy

1. Check the soju config has `message-store db` (and `db sqlite3 ...`).
2. Back up the database file named on the `db` line.
3. Stop soju, replace the `soju` and `sojuctl` binaries with the ones in `out/`, start soju.
   Nothing changes for existing users until one of them sets a key.

## License

soju is AGPL-3.0, and so is this patch. If you run it for other people, they're entitled to its
source: point them here (this patch plus soju v0.11.1) or to your own copy.
