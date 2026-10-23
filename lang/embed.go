// Package lang, dil dosyalarının kaynak klasörüdür. Varsayılan dil olan
// İngilizce exe'ye gömülür; diğer diller çalışma anında exe'nin yanındaki
// lang\ klasöründen okunur (build sırasında bu klasör oraya kopyalanır).
package lang

import _ "embed"

// English, gömülü varsayılan dil dosyasıdır (en.lng).
//
//go:embed en.lng
var English []byte
