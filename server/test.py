import p5

def setup():
   p5.size(128,64)
   p5.no_loop()
   p5.fill(0)

def draw():
   p5.background(204)
   p5.text("LAX", (0, 10))
   p5.text("LHR", (0, 70))
   p5.text("TXL", (0, 100))
   
p5.run(sketch_setup=setup,sketch_draw=draw,renderer="skia")
