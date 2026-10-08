# sing-box для Keenetic / Netсraze

> [!IMPORTANT]
> Если на сервере используется Xray версии 26.9.8 или выше и при подключении через REALITY не работает отпечаток `chrome`, попробуйте отпечаток `randomized`.

Сборки sing-box под роутеры Keenetic и Netсraze с дополнительными патчами.

Это не исходный код upstream `sing-box`, а релизный проект для сборки готовых бинарников под архитектуры, которые обычно используются в маршрутизаторах семейства Keenetic / Netсraze. Основная цель — собрать sing-box с нужными тегами и патчами, проверить REALITY ClientHello и выложить артефакты в GitHub Release.

[English version](README.md)

## Что это за проект

Репозиторий автоматически:

- получает нужную версию upstream `SagerNet/sing-box` по тегу;
- накладывает патч [.github/patches/reality.patch](.github/patches/reality.patch);
- собирает бинарники под целевые архитектуры;
- добавляет нужные `build tags`;
- при необходимости сжимает бинарники `UPX`;
- проверяет, что собранный бинарник проходит REALITY-проверку;
- публикует результаты в GitHub Release.

## Поддерживаемые сборки

В GitHub Actions собираются бинарники для следующих конфигураций:

- `linux/arm64` + `musl`
- `linux/mipsle` + `softfloat` + `musl`
- `linux/mips` + `softfloat`

Для `arm64` и `mipsle` дополнительно включаются теги:

- `with_naive_outbound`
- `with_musl`

Для всех сборок используется набор общих тегов:

- `with_quic`
- `with_utls`
- `with_clash_api`
- `badlinkname`
- `tfogo_checklinkname0`
- `with_gvisor` для стабильных релизов

UPX-версии публикуются с суффиксом `_upx`, если сжатие прошло проверку.

## Применяемые патчи

1. Патч [.github/patches/reality.patch](.github/patches/reality.patch) предназначен для REALITY-совместимости с Chrome/randomized fingerprints:

- сохраняет `X25519MLKEM768` hybrid share для `chrome`-совместимого ClientHello;
- стабилизирует `key share` и `ALPN` для `randomized` fingerprint;
- корректирует идентификацию версии REALITY-клиента;
- делает поведение ближе к ожидаемому от реального Chrome/Xray client hello.

То есть решает проблему, когда upstream `sing-box`/`utls` формирует ClientHello без нужного hybrid-ключа или с неверной расстановкой ключей, и REALITY на сервере с Xray отбрасывает соединение.

## Проверка перед релизом

Workflow [.github/workflows/build.yml](.github/workflows/build.yml) выполняет двойную проверку:

1. загружает официальный upstream `sing-box` release для версии;
2. убеждается, что исходный, неподпатченный бинарник падает в REALITY-проверке;
3. собирает патченный бинарник;
4. прогоняет его через проверку `.github/verify/reality/verify.sh`;
5. публикует только корректные артефакты в GitHub Release.

Это делает релиз более предсказуемым и защищает от попадания в release бинарников, которые не проходят REALITY-тест.

## Лицензия

Проект распространяется под лицензией MIT. См. [LICENSE](LICENSE).
