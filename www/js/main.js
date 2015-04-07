var temperatureIDs=[];
var gauges = {}
var updateInterval = 3000; //3 seconds
var tempMin = 0.0;
var tempMax = 0.0;
var tempTarget = 0.0;
var probeInterval = 0;
var recordInterval = 0;


function initHandler() {
	//get the list of temeratures and populate globals
	$.getJSON( "/api/config", function( data ) {
		tempMax = data.HighTemp;
		tempMin = data.LowTemp;
		tempTarget = data.TargetTemp;
		probeInterval = data.ProbeInterval;
		recordInterval = data.RecordInterval;
		$.each(data.Probes, function(k, v) {
			temperatureIDs.push(v);
		});
		//build out gauges
		buildGauges();
		//kick off an update
		tempUpdate();

		//build graphs
		buildGraphs();
	}).fail(function(error) {
		console.log("Failed to get config");
		console.log(error);
	});
}

function buildGauges() {
	min = tempMin;
	max = tempMax;
	$.each(temperatureIDs, function(k, v){
		divID=v.ID
		nd = '<div class="col-sx-12 col-sm-6 col-md-3 col-lg-3" align="center">' +
		'<div id="'+ divID +'gauge"></div>' +
		'<h4>'+v.Alias+'</h4>' +
		'<span class="text-muted" id="'+ v.ID +'temp">0.0 &deg;</span>'
		'</div>';
		$("#gauges").append(nd);
		if (!(v.MinOverride == 0.0 && v.MaxOverride == 0.0)) {
			min = v.MinOverride;
			max = v.MaxOverride;
		}
		var g = new JustGage({
			id: v.ID+'gauge',
			value: 0,
			min: min,
			max: max,
			title: v.Alias+" Temperature",
			levelColors: ["#ff0000", "#ffff00", "#00ff00", "#ffff00", "#ff0000"],
			valueFontColor: "#FFFFFF"
		});
		gauges[v.ID] = g
	});
}

function updateKegTemp(ID, value) {
	if (ID in gauges) {
		gauges[ID].refresh(value);
		$('#'+ID+'temp').html(value + " &deg; C");
	} else {
		console.log("Could not find any record of "+ID);
		console.log(gauges);
	}
}

function tempUpdate() {
	$.getJSON( "/api/temps/now", function( data ) {
		$.each(data, function(k, v) {
			updateKegTemp(k, v);
		});
	}).fail(function(error) {
		console.log("Failed to update temps "+error);
	});

	$.getJSON( "/api/compressor/now", function(data) {
		updateCompressorState(data);
	}).fail(function(error) {
		console.log("Failed to update compressor state "+error);
	});

	//do this last so if it bombs we don't just keep hitting the backend
	setTimeout(tempUpdate, updateInterval);
}

function updateCompressorState(state) {
	var s = "OFF"
	var color = "#ff0000"

	if(state == true) {
		s = "ON"
		color = "#00ff00"
	}
	$('#compressor_state').html(s)
	$('#compressor_state').attr('color', color)
}

function buildGraphs() {
	buildTemperatureGraphs();
	buildCompressorGraphs();
}

function buildTemperatureGraphs() {
	var readings = [];
	var labels = [];
	
	$.getJSON( "/api/temps/all", function( data ) {
		if(data.length == 0) {
			return;
		}
		labels.push("Time");
		$.each(data[0].Temps, function(k, v) {
			labels.push(k);
		});
		$.each(data, function(i, v) {
			var t = new Date(data[i].TS)
			var vals = [t]
			$.each(data[i].Temps, function(j, temp) {
				vals.push(data[i].Temps[j])
			});
			readings.push(vals)
		});
	}).fail(function(error) {
		console.log("Failed to gather temp stamps "+error);
	}).done(function() {
		var dg = new Dygraph(document.getElementById("tempgraph"), readings,
                   {
		      title: "Temperatures",
                      drawPoints: true,
                      showRoller: true,
                      labels: labels
                   }
                );
                dg.updateOptions({'file': readings});
	});
}

function buildCompressorGraphs() {
	var readings = [];
	var labels = ["Time", "Compressor Activity"];
	
	$.getJSON( "/api/compressor/all", function( data ) {
		if(data.length == 0) {
			return;
		}
		$.each(data, function(i, v) {
			var start = new Date(data[i].Start)
			var stop = new Date(data[i].Stop)
			var timeOn = (stop.getTime() - start.getTime())/1000
			readings.push([start, timeOn])
		});
	}).fail(function(error) {
		console.log("Failed to gather temp stamps "+error);
	}).done(function() {
		var dg = new Dygraph(document.getElementById("compressorgraph"), readings,
                   {
		      title: "Compressor Engaged",
                      drawPoints: true,
                      showRoller: true,
                      labels: labels
                   }
                );
                dg.updateOptions({'file': readings});
	});
}
