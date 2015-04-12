# kegarator
A smart kegarator that tracks temperature and controls the compressor with a web interface and stats

## purpose
Good beers need precise temperature control, and anal retentive beer drinkers need to know exactly
what their beers are doing.  So I built a kegarator with a proper control system that allows me to
fine tune my temperatures and track exactly what my beers are doing.  The kit is based on a standard
Lowes chest freezer and a kegarator kit from beveragefactory.com with the control system built with
a raspberrypi A+.  Temperature sensing is done via a few waterproofed <a href="https://www.sparkfun.com/products/11050">DS18B20 sensors</a>
and a <a href="https://www.sparkfun.com/products/11042">Beefcake relay</a>.  I control the beefcake
relay via a <a href="https://www.sparkfun.com/products/11771">3.3V to 5V level shifter</a>.
They system allows for as many temperature sensors as the 1-wire bus will take.  My setup has
two sensors on the kegs and one sensor in the control system enclosure, so I can track keg
temperatures as well as know that my control system isn't overheating.  The config file allows
for specifying which sensors determine compressor action.

## Interface
The web interface is based on Twitter Bootstrap and uses a few graph and guage components.
Most of the component licenses are MIT licensed and those that aren't are MIT compatible.
The control system code is all GO and fully MIT licensed.

## Result
My beer is the perfect temperature at all times and I know exactly what the temperature has been
doing.  I also know exactly how often I run the compressor and exactly how much it costs me to
run my kegarator (except beer, that is priceless).

## Alternate Uses
If you want to distill rather than keep beers frosty, strap the relay to a burner or heater coil
and adjust temperatures as needed (minor logic inversion required).  If someone is interested
in rigging the system up to heat rather than cool I'll happily throw in a switch in the config
to invert the logic.
