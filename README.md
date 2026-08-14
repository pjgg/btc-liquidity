# btc-liquidity

Una página estática que contrasta el precio de Bitcoin con la liquidez de los bancos
centrales, buscando el desfase temporal que mejor los relaciona. Sin servidor, sin build
y sin claves de API: un GitHub Action refresca los CSV a diario y GitHub Pages sirve el
resultado.

## Qué responde

**¿La liquidez adelanta a Bitcoin, y con cuántos días?** El desfase no se asume: se prueban
todos los valores entre 0 y 180 días y se elige el que maximiza la correlación histórica.

La correlación se mide entre **variaciones logarítmicas a 30 días**, no entre niveles. Dos
series con tendencia alcista correlacionan cerca de 0,9 aunque no tengan ninguna relación,
así que un ajuste sobre niveles devolvería un desfase sin significado.

### El resultado, hoy

Con los datos desde 2018 y Fed Net Liquidity, el desfase óptimo sale en torno a **20 días**
con una correlación de **~0,18**: una relación **muy débil**. No son los 70-90 días de la
tesis que suele circular. La página lo dice en su propia cara en lugar de disimularlo — el
intervalo de predicción al 95 % abarca ±222 %, que es tanto como no predecir nada.

Sigue siendo útil para ver la forma de ambas series y para comprobar la afirmación por uno
mismo. No es una herramienta de decisión, y la página lo advierte.

## Cómo funciona

```
.github/workflows/update.yml   cron diario -> go run ./cmd/update -> commit de data/
cmd/update/                    ejecutable que refresca los CSV
internal/liquidity/            normalización de unidades y series derivadas
internal/sources/              descarga y parseo de FRED y Binance
internal/store/                los CSV que hacen de base de datos
internal/pipeline/             ensamblado a tabla diaria
data/                          btc.csv, liquidity.csv, meta.json
index.html                     la página: un fichero, JS embebido
```

El navegador **sólo lee**. La escritura la hace el Action con su propio `GITHUB_TOKEN`, así
que no hay ningún secreto en la página — que además es pública y cualquiera podría leer.

## Fuentes

Todas sin clave de API, verificadas el 2026-08-14.

| Serie | Origen | Frecuencia | Unidad publicada |
|---|---|---|---|
| `BTCUSDT` | `api.binance.com` klines | diaria | USD |
| `WALCL` balance de la Fed | `fredgraph.csv` | semanal | Millones USD |
| `WTREGEN` cuenta del Tesoro | `fredgraph.csv` | semanal | Millones USD |
| `RRPONTSYD` repo inverso | `fredgraph.csv` | diaria | **Miles de millones** USD |
| `ECBASSETSW` balance del BCE | `fredgraph.csv` | semanal | **Millones de EUR** |
| `JPNASSETS` balance del BoJ | `fredgraph.csv` | mensual | **100 millones de YEN** |
| `DEXUSEU`, `DEXJPUS` | `fredgraph.csv` | diaria | tipos de cambio |

Las unidades son distintas entre series y **eso es el mayor riesgo del proyecto**: sumarlas
sin normalizar da un número que parece razonable y es falso. Todo se convierte a millones de
USD en `internal/liquidity`, una sola vez, y los tests fijan cada conversión contra una
observación real.

Dos detalles que costaron encontrar:

- FRED publica **valores vacíos** en festivos (`2026-01-01,`). Leerlos como `0.0` multiplica
  por cero el balance convertido a ese tipo de cambio, y la curva se desploma un día de cada
  veinte. Se saltan.
- Cada balance se convierte al tipo de cambio **de su propia fecha de observación**. Convertir
  una cifra del BoJ de hace seis semanas al tipo de hoy inventaría un salto en el agregado
  global cada vez que se moviera el yen.

### Lo que no se pudo usar

No existe una serie de M2 global accesible: `MYAGM2EZM196N` (BCE) acaba en 2017,
`MYAGM2JPM189S` (Japón) en 2017, `MYAGM2CNM189N` (China) en 2019, y no hay ninguna serie del
PBoC en FRED. Por eso el agregado son **balances de bancos centrales** y **China queda fuera**.

## Uso local

```bash
go test ./...            # las conversiones de unidades y los parsers
go run ./cmd/update      # refresca data/ desde las fuentes reales
python3 -m http.server 8811   # y abrir http://localhost:8811
```

`go run ./cmd/update --start 2020-01-01` cambia el inicio del histórico. Reejecutar el mismo
día corrige la fila de ese día en lugar de duplicarla.

## Advertencia

Esto no es asesoramiento financiero. La envolvente ±5 % es un margen elegido a mano sin
significado estadístico; el intervalo al 95 % sí se deriva de los residuos del ajuste.
Correlación no es causalidad, y un desfase que funcionó en el pasado puede dejar de hacerlo.
