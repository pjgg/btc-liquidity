# btc-liquidity

Una página estática que contrasta el precio de Bitcoin con la liquidez de los bancos
centrales, buscando el desfase temporal que mejor los relaciona. Sin servidor, sin build
y sin claves de API: un GitHub Action refresca los CSV a diario y GitHub Pages sirve el
resultado.

## Qué responde

**¿Bitcoin sigue a la liquidez, y con cuánto retraso?** Se dibujan las dos series reales, sin
predicción, con la liquidez desplazada hacia delante los días que elijas (7 por defecto): sobre
cada fecha aparece la liquidez de ese número de días antes.

Ambas curvas se indexan a 100 al inicio del rango porque están en unidades distintas —dólares
por bitcoin y billones de dólares—. **No hay segundo eje Y**, y eso es deliberado: es lo que
diferencia esta página de los gráficos que circulan en vídeo, donde una línea de liquidez va
superpuesta al eje de precios. Ahí la herramienta estira el símbolo superpuesto hasta llenar el
panel, así que un movimiento real del 4-5 % se dibuja tan grande como uno del 40 % del precio y
la curva parece anticipar cada giro. Con dos escalas independientes se puede alinear
visualmente cualquier par de curvas.

La correlación se mide **al desfase que elijas**, entre variaciones logarítmicas a 30 días.
Dos decisiones detrás de eso:

- **Variaciones y no niveles.** Dos series con tendencia alcista correlacionan cerca de 0,9 sin
  tener ninguna relación.
- **Al desfase elegido y no al mejor de 181.** Buscar el máximo entre todos los desfases posibles
  encontraba correlaciones de 0,91 sobre rangos cortos que eran puro azar — con 226 días y un
  desfase de 128 quedan 68 muestras solapadas, unas 2 independientes. Si el desfase lo fijas tú,
  esa trampa desaparece.

La serie que se muestra al abrir es **la que mejor correlaciona** de las cuatro, medido en lugar
de elegido a mano.

### El resultado, hoy

Correlación a 7 días de desfase sobre el histórico completo desde 2018:

| Serie | r |
|---|---|
| Reservas bancarias | +0,173 |
| Fed Net Liquidity | +0,160 |
| Liquidez global | +0,120 |
| Balance de la Fed | +0,071 |

**Las cuatro son positivas**, así que la dirección se sostiene: la liquidez va por delante y
correlaciona positivamente con el precio. Pero la fuerza es débil. Para la liquidez global el
máximo está en **46 días con r = +0,196**, subiendo desde 0,100 sin desfase: hay señal de
adelanto, mucho más leve de lo que sugieren esos vídeos.

Indexado a una base común desde 2018, BTC hizo ×10 y la liquidez ×1,5.

## ¿Inyectan o drenan?

La página incluye un panel que responde esto con los flujos de la Reserva Federal a 4, 13 y
52 semanas. Lo interesante es que el signo no es el mismo en todos: el balance de la Fed y el
repo inyectan al subir, mientras que la cuenta del Tesoro y el repo inverso drenan al subir.
El panel traduce cada uno a su lectura en vez de dejar los números crudos.

El titular sale de las **reservas bancarias**, que son donde acaba el saldo de todos los
flujos. Con los datos actuales la Fed está expandiendo su balance y aun así las reservas caen,
porque el Tesoro absorbe más de lo que la Fed mete: *que el banco central inyecte* y *que haya
más liquidez en los bancos* son cosas distintas, y ahora mismo van en direcciones opuestas.

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
| `DEXUSEU`, `DEXJPUS`, `DEXCHUS` | `fredgraph.csv` | diaria | tipos de cambio |
| PBoC activos totales | `api.db.nomics.world` `NBS/A_A0L05/A0L0501` | **anual** | **100 millones de YUAN** |
| `WRESBAL` reservas bancarias | `fredgraph.csv` | semanal | Millones USD |
| `RPONTSYD` repo | `fredgraph.csv` | diaria | **Miles de millones** USD |
| `WLCFLPCL` ventanilla de descuento | `fredgraph.csv` | semanal | Millones USD |

Las unidades son distintas entre series y **eso es el mayor riesgo del proyecto**: sumarlas
sin normalizar da un número que parece razonable y es falso. Todo se convierte a millones de
USD en `internal/liquidity`, una sola vez, y los tests fijan cada conversión contra una
observación real.

**`RPONTSYD` y `RRPONTSYD` se diferencian en una letra y significan lo contrario**: el repo
inverso es efectivo que sale del sistema hacia la Fed, y el repo es efectivo que la Fed presta
al sistema. Cruzarlos invertiría el signo de la liquidez neta produciendo un gráfico que sigue
pareciendo verosímil. Hay un test que lo fija.

**China está dentro del agregado global.** Es la pieza que hace que el número cuadre: Fed 6,76 +
BCE 6,85 + BoJ 3,97 + PBoC 6,04 = **23,61 B$**, frente a los 24,04 B$ que muestran los gráficos
que circulan. Sin China salía 17,6 y no era comparable.

El precio es que la serie del PBoC es **anual** y se publica con más de un año de retraso (el
último dato real es 2024-12-31, y 2025 viene como `NA`). Su valor se arrastra entre escalones, de
modo que una cuarta parte del agregado avanza una vez al año y está plana el resto del tiempo. La
página muestra la antigüedad del componente más atrasado de cada serie y avisa cuando pasa de 45
días, para que esa cola plana no se lea como que la liquidez se ha quedado quieta.

Tres detalles más que costaron encontrar:

- FRED publica **valores vacíos** en festivos (`2026-01-01,`). Leerlos como `0.0` multiplica
  por cero el balance convertido a ese tipo de cambio, y la curva se desploma un día de cada
  veinte. Se saltan.
- Cada balance se convierte al tipo de cambio **de su propia fecha de observación**. Convertir
  una cifra del BoJ de hace seis semanas al tipo de hoy inventaría un salto en el agregado
  global cada vez que se moviera el yen.
- Esa búsqueda del tipo tiene que **mirar hacia atrás**, no exigir el mismo día exacto. El PBoC
  fecha sus balances a 31 de diciembre, que cayó en fin de semana en 4 de sus 9 años, y el BoJ
  publica a día 1 de mes, que es fin de semana un tercio de las veces. Exigir el tipo del mismo
  día descartaba esas observaciones en silencio: del PBoC sobrevivían 5 de 9.

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
